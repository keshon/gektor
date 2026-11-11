// Package roles builds the agents this project runs.
//
// Role constructors share one environment, tool policy, and context-aware gate.
// Frontends provide observers and transport; capabilities are chosen here.
//
// This lives outside internal/agent on purpose: agent defines the Tool
// interface that tools implements, so agent cannot import tools without a
// cycle. roles is the layer that knows about both.
package roles
