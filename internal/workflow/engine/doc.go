// Package engine executes a frozen definition through an operation handler.
// The caller supplies authorization and persistence. Checkpoints precede
// effects, and an uncertain operation is never automatically replayed.
package engine
