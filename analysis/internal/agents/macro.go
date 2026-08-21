package agents

// CycleNarrative is the persisted narrative context made available to the
// two cycle-level agents. It keeps the agents independent of storage.
type CycleNarrative struct {
	AgentType string
	Asset     string
	Narrative string
}

type MacroIndicators struct {
	AssetsAnalyzed int `json:"assets_analyzed"`
	SourceCount    int `json:"source_count"`
}
