package cell

// Separator keeps application metadata distinct from punctuation in source content.
func Separator(square bool) string {
	if square {
		return " ▪ "
	}
	return " · "
}
