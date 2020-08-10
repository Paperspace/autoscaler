package paperspace

type Filter struct {
	Where map[string]interface{} `json:"where"`
	Limit int64                  `json:"limit"`
	Order string                 `json:"order"`
}
