// Package service loads and freezes recipes, composes nested workflows and
// applies the caller's authorization before execution. CLI, API, agent tools
// and TUI use this layer. It connects the engine to the workspace and store;
// those packages do not construct application clients or operator gates.
package service
