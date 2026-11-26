//go:build !race

package markdown

import "time"

const prettierTimeout = 200 * time.Millisecond
