package agents

// Output contains structured indicators and the optional LLM narrative.
// Err reports a narrative failure after indicators were computed.
type Output struct {
	Indicators any
	Narrative  string
	Err        error
}
