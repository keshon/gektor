package llm

// ModelFacts is a snapshot. Unknown values never overwrite previously known facts.
type ModelFacts struct {
	Model                                 string
	ContextLimit, ContextWindow           int
	ContextEstimated                      bool
	Vision                                bool
	ModelKnown, ContextKnown, VisionKnown bool
}
