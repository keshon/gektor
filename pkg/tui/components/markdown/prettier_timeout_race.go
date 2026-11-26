//go:build race

package markdown

import "time"

// Race instrumentation slows down Goja's plugin initialization and execution.
const prettierTimeout = 3 * time.Second
