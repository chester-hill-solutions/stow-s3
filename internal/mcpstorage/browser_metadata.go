package mcpstorage

type browserToolUI struct {
	ResourceURI string   `json:"resourceUri,omitempty"`
	Visibility  []string `json:"visibility,omitempty"`
}
type browserEntry struct {
	Type string `json:"type"`
}
type browserEntrypoints struct {
	Entrypoints []browserEntry `json:"entrypoints"`
}
type browserMentions struct {
	Search struct{} `json:"mentions/search"`
}
type browserCSP struct {
	ConnectDomains  []string `json:"connectDomains"`
	ResourceDomains []string `json:"resourceDomains"`
}
type browserResourceUI struct {
	CSP browserCSP `json:"csp"`
}
