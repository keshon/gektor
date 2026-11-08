package clamp

// Clamp returns value bounded by low and high. Callers supply low <= high.
func Clamp(value, low, high int) int {
	if value < low {
		return high
	}
	if value > high {
		return high
	}
	return value
}
