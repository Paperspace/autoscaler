/*
Copyright 2020 The Kubernetes Authors.

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

package clusterapi

import (
	"testing"
)

func TestSetSize(t *testing.T) {
	initialReplicas := 1
	updatedReplicas := int32(5)

	test := func(t *testing.T, testConfig *testConfig) {
		controller, stop := mustCreateTestController(t, testConfig)
		defer stop()

		testResource := testConfig.machineSet
		if testConfig.machineDeployment != nil {
			testResource = testConfig.machineDeployment
		}

		sr, err := newUnstructuredScalableResource(controller, testResource)
		if err != nil {
			t.Fatal(err)
		}

		gvr, err := sr.GroupVersionResource()
		if err != nil {
			t.Fatal(err)
		}

		err = sr.SetSize(updatedReplicas)
		if err != nil {
			t.Fatal(err)
		}

		s, err := sr.controller.managementScaleClient.Scales(testResource.GetNamespace()).
			Get(gvr.GroupResource(), testResource.GetName())
		if err != nil {
			t.Fatal(err)
		}

		if s.Spec.Replicas != updatedReplicas {
			t.Errorf("expected %v, got: %v", updatedReplicas, s.Spec.Replicas)
		}
	}

	t.Run("MachineSet", func(t *testing.T) {
		test(t, createMachineSetTestConfig(RandomString(6), RandomString(6), initialReplicas, nil))
	})

	t.Run("MachineDeployment", func(t *testing.T) {
		test(t, createMachineDeploymentTestConfig(RandomString(6), RandomString(6), initialReplicas, nil))
	})
}

func TestReplicas(t *testing.T) {
	initialReplicas := 1
	updatedReplicas := int32(5)

	test := func(t *testing.T, testConfig *testConfig) {
		controller, stop := mustCreateTestController(t, testConfig)
		defer stop()

		testResource := testConfig.machineSet
		if testConfig.machineDeployment != nil {
			testResource = testConfig.machineDeployment
		}

		sr, err := newUnstructuredScalableResource(controller, testResource)
		if err != nil {
			t.Fatal(err)
		}

		gvr, err := sr.GroupVersionResource()
		if err != nil {
			t.Fatal(err)
		}

		i, err := sr.Replicas()
		if err != nil {
			t.Fatal(err)
		}

		if i != int32(initialReplicas) {
			t.Errorf("expected %v, got: %v", initialReplicas, i)
		}

		// fetch and update machineSet
		s, err := sr.controller.managementScaleClient.Scales(testResource.GetNamespace()).
			Get(gvr.GroupResource(), testResource.GetName())
		if err != nil {
			t.Fatal(err)
		}

		s.Spec.Replicas = updatedReplicas

		_, err = sr.controller.managementScaleClient.Scales(testResource.GetNamespace()).
			Update(gvr.GroupResource(), s)
		if err != nil {
			t.Fatal(err)
		}

		i, err = sr.Replicas()
		if err != nil {
			t.Fatal(err)
		}

		if i != updatedReplicas {
			t.Errorf("expected %v, got: %v", updatedReplicas, i)
		}
	}

	t.Run("MachineSet", func(t *testing.T) {
		test(t, createMachineSetTestConfig(RandomString(6), RandomString(6), initialReplicas, nil))
	})

	t.Run("MachineDeployment", func(t *testing.T) {
		test(t, createMachineDeploymentTestConfig(RandomString(6), RandomString(6), initialReplicas, nil))
	})
}

func TestSetSizeAndReplicas(t *testing.T) {
	initialReplicas := 1
	updatedReplicas := int32(5)

	test := func(t *testing.T, testConfig *testConfig) {
		controller, stop := mustCreateTestController(t, testConfig)
		defer stop()

		testResource := testConfig.machineSet
		if testConfig.machineDeployment != nil {
			testResource = testConfig.machineDeployment
		}

		sr, err := newUnstructuredScalableResource(controller, testResource)
		if err != nil {
			t.Fatal(err)
		}

		i, err := sr.Replicas()
		if err != nil {
			t.Fatal(err)
		}

		if i != int32(initialReplicas) {
			t.Errorf("expected %v, got: %v", initialReplicas, i)
		}

		err = sr.SetSize(updatedReplicas)
		if err != nil {
			t.Fatal(err)
		}

		i, err = sr.Replicas()
		if err != nil {
			t.Fatal(err)
		}

		if i != updatedReplicas {
			t.Errorf("expected %v, got: %v", updatedReplicas, i)
		}
	}

	t.Run("MachineSet", func(t *testing.T) {
		test(t, createMachineSetTestConfig(RandomString(6), RandomString(6), initialReplicas, nil))
	})

	t.Run("MachineDeployment", func(t *testing.T) {
		test(t, createMachineDeploymentTestConfig(RandomString(6), RandomString(6), initialReplicas, nil))
	})
}
