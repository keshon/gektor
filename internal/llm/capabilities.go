package llm

// ModelFacts is a snapshot. Unknown values never overwrite previously known facts.
type ModelFacts struct {
	Model                                 string
	ContextLimit                          int
	Vision                                bool
	ModelKnown, ContextKnown, VisionKnown bool
}
