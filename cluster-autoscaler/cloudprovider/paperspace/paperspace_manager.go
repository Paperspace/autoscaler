/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package paperspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"strconv"
	"strings"

	psgo "github.com/paperspace/paperspace-go"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	"k8s.io/autoscaler/cluster-autoscaler/utils/gpu"
	"k8s.io/klog"
	kubeletapis "k8s.io/kubernetes/pkg/kubelet/apis"
)

type nodeGroupClient interface {
	// GetNodePools lists all the node pools found in a Kubernetes cluster.
	GetAutoscalingGroups(params psgo.AutoscalingGroupListParams) ([]psgo.AutoscalingGroup, error)

	// UpdateNodePool updates the details of an existing node pool.
	UpdateAutoscalingGroup(id string, params psgo.AutoscalingGroupUpdateParams) error

	// DeleteNode deletes a specific node in a node pool.
	DeleteMachine(id string, params psgo.MachineDeleteParams) error
}

var _ nodeGroupClient = (*psgo.Client)(nil)

// Manager handles Paperspace communication and data caching of
// node groups (node pools in DOKS)
type Manager struct {
	client     nodeGroupClient
	clusterID  string
	nodeGroups []*NodeGroup
}

// Config is the configuration of the Paperspace cloud provider
type Config struct {
	// ClusterID is the id associated with the cluster where Paperspace
	// Cluster Autoscaler is running.
	ClusterID string `json:"clusterId"`

	// Token is the User's Access Token associated with the cluster where
	// Paperspace Cluster Autoscaler is running.
	APIKey string `json:"apiKey"`

	// URL points to Paperspace API. If empty, defaults to
	// https://api.paperspace.com/
	URL string `json:"url"`

	// URL points to Paperspace API. If empty, defaults to false
	Debug bool `json:"debug"`
}

func newManager(configReader io.Reader, nodeGroupSpecs []string, do cloudprovider.NodeGroupDiscoveryOptions, instanceTypes map[string]string) (*Manager, error) {
	cfg := &Config{}
	if configReader != nil {
		body, err := ioutil.ReadAll(configReader)
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(body, cfg)
		if err != nil {
			return nil, err
		}
	}

	if cfg.APIKey == "" {
		return nil, errors.New("access token is not provided")
	}

	apiBackend := psgo.NewAPIBackend()
	if cfg.URL != "" {
		apiBackend.BaseURL = cfg.URL
	}
	if cfg.Debug {
		apiBackend.Debug = cfg.Debug
	}

	client := psgo.NewClientWithBackend(apiBackend)
	client.APIKey = cfg.APIKey

	//specs, err := do.ParseASGAutoDiscoverySpecs()
	//if err != nil {

	// parse static options
	var nodeGroups []*NodeGroup
	for _, nodeGroupSpec := range nodeGroupSpecs {
		specs := strings.Split(nodeGroupSpec, ":")
		if len(specs) != 3 {
			return nil, errors.New(fmt.Sprintf("Static ASG definition invalid: %s", specs))
		}
		min, _ := strconv.Atoi(specs[0])
		max, _ := strconv.Atoi(specs[1])
		id := specs[2]
		nodeGroups = append(nodeGroups, &NodeGroup{
			id:        id,
			clusterID: cfg.ClusterID,
			manager:   nil,
			asg:       psgo.AutoscalingGroup{},
			minSize:   min,
			maxSize:   max,
		})
	}

	m := &Manager{
		client:     client,
		clusterID:  cfg.ClusterID,
		nodeGroups: nodeGroups,
	}

	return m, nil
}

// Refresh refreshes the cache holding the nodegroups. This is called by the CA
// based on the `--scan-interval`. By default it's 10 seconds.
func (m *Manager) Refresh() error {
	ctx := context.Background()
	params := psgo.AutoscalingGroupListParams{
		RequestParams: psgo.RequestParams{Context: ctx},
		Filter:        nil,
		IncludeNodes:  true,
	}
	if len(m.nodeGroups) > 0 {
		var ids []string
		for _, nodeGroup := range m.nodeGroups {
			ids = append(ids, nodeGroup.id)
		}
		params.Filter = make(map[string]string, 1)
		params.Filter["where"] = fmt.Sprintf(`id: { inq: ["%s"] }`, strings.Join(ids, `", "`))
	}
	autoscalingGroups, err := m.client.GetAutoscalingGroups(params)
	if err != nil {
		return err
	}

	var groups []*NodeGroup
	for _, asg := range autoscalingGroups {
		klog.V(4).Infof("adding node pool: %q name: %s min: %d max: %d",
			asg.ID, asg.Name, asg.Min, asg.Max)

		groups = append(groups, &NodeGroup{
			id:        asg.ID,
			clusterID: m.clusterID,
			manager:   m,
			asg:       asg,
			minSize:   asg.Min,
			maxSize:   asg.Max,
		})
	}

	if len(groups) == 0 {
		klog.V(4).Info("cluster-autoscaler is disabled. no node pools are configured")
	}

	m.nodeGroups = groups
	return nil
}

type vmType struct {
	Label string
	CPU   int64
	GPU   int64
	RAM   int64
}

func (m *Manager) getMachineType(machineType string) vmType {
	// TODO real solution
	if machineType == "C5" {
		return vmType{
			Label: "C5",
			CPU:   4,
			GPU:   0,
			RAM:   8589934592,
		}
	}
	return vmType{}
}

func (m *Manager) buildGenericLabels(vmType vmType, nodeName string) map[string]string {
	result := make(map[string]string)
	// TODO: extract it somehow
	result[kubeletapis.LabelArch] = cloudprovider.DefaultArch
	result[kubeletapis.LabelOS] = cloudprovider.DefaultOS
	result[apiv1.LabelInstanceType] = vmType.Label
	result[apiv1.LabelHostname] = nodeName
	return result
}

func (m *Manager) buildNodeFromTemplate(asg psgo.AutoscalingGroup) (*apiv1.Node, error) {
	node := apiv1.Node{}
	nodeName := fmt.Sprintf("%s-tmpl-%d", asg.ID, rand.Int63())

	node.ObjectMeta = metav1.ObjectMeta{
		Name:     nodeName,
		SelfLink: fmt.Sprintf("/api/v1/nodes/%s", nodeName),
		Labels:   map[string]string{},
	}

	node.Status = apiv1.NodeStatus{
		Capacity: apiv1.ResourceList{},
	}

	vmType := m.getMachineType(asg.MachineType)

	// TODO: get a real value.
	node.Status.Capacity[apiv1.ResourcePods] = *resource.NewQuantity(110, resource.DecimalSI)
	node.Status.Capacity[apiv1.ResourceCPU] = *resource.NewQuantity(vmType.CPU, resource.DecimalSI)
	node.Status.Capacity[gpu.ResourceNvidiaGPU] = *resource.NewQuantity(vmType.GPU, resource.DecimalSI)
	node.Status.Capacity[apiv1.ResourceMemory] = *resource.NewQuantity(vmType.RAM, resource.DecimalSI)

	// TODO: use proper allocatable!!
	node.Status.Allocatable = node.Status.Capacity

	// NodeLabels
	//node.Labels = cloudprovider.JoinStringMaps(node.Labels, extractLabelsFromAsg(template.Tags))
	// GenericLabels
	node.Labels = cloudprovider.JoinStringMaps(node.Labels, m.buildGenericLabels(vmType, nodeName))
	node.Labels[poolNameLabel] = "metal-cpu"
	node.Labels[poolTypeLabel] = "cpu"
	if vmType.GPU > 0 {
		node.Labels[poolNameLabel] = "metal-gpu"
		node.Labels[poolTypeLabel] = "gpu"
	}

	//node.Spec.Taints = extractTaintsFromAsg(template.Tags)

	node.Status.Conditions = cloudprovider.BuildReadyConditions()
	return &node, nil
}
