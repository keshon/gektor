package ledger

import (
	"strconv"
	"strings"
)

// TotalCents adds decimal money amounts and returns their sum in cents.
func TotalCents(values []string) (int, error) {
	total := 0
	for _, value := range values {
		cents, err := strconv.Atoi(strings.ReplaceAll(value, ".", ""))
		if err != nil {
			return 0, err
		}
		total += cents
	}
	return total, nil
}
