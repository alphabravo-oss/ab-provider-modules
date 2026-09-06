package resource

type Alias struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

type Resource struct {
	Type    string         `json:"type"`
	UID     string         `json:"uid"`
	Account string         `json:"account,omitempty"`
	Region  string         `json:"region,omitempty"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Aliases []Alias        `json:"aliases,omitempty"`
	Edges   []Edge         `json:"edges,omitempty"`
}
