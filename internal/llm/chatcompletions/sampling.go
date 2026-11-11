package chatcompletions

// Default sampling. These are sent on every request so that nothing is
// left to whatever preset the server happened to start with: a local
// backend silently supplies its own defaults for any field omitted, and
// a score measured under unknown sampling cannot be compared to the next
// one. Values here are deliberate and measurable, not inherited.
//
// top_p and top_k are here for a sharper version of that reason. They
// were omitted, so each backend applied its own — and koboldcpp and
// llama-server do not agree on them. A comparison between the two
// backends would have been partly a comparison of their sampler presets,
// which is exactly the confound the temperature default was added to
// remove.
//
// DRY is off by default, and that is a measured decision rather than a
// preference. It was switched on to stop a collapse — a run whose context
// filled with high-entropy base64 degenerated into one token repeated for
// hundreds of lines — and the next baseline showed every probe that
// patches a file getting slower and less reliable while every probe that
// does not stayed byte-identical.
//
// The mechanism fits: DRY penalizes verbatim repetition of sequences, and
// patch_file requires reproducing old_content exactly. Repetition is
// degeneration in prose and correctness in code, so a sampler that cannot
// tell them apart should not be on by default in a coding agent.
// KoboldClient.DRY re-enables it for anyone who wants to re-test.
const (
	defaultTemperature = 0.4
	defaultTopP        = 0.95
	defaultTopK        = 40
	defaultRepPen      = 1.05
	defaultRepPenRange = 1024
	defaultDRYMult     = 0.8
	defaultDRYBase     = 1.75
	defaultDRYAllowed  = 2
)
