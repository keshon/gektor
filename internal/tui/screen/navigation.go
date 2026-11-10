package screen

// Menu names one group of application commands. Screens supply their local
// groups; the terminal host adds shared navigation, settings, and help.
type Menu struct {
	ID, Label string
}

// MenuItem describes a command's current availability. An empty item separates
// adjacent groups; rendering and dispatch belong to the terminal host.
type MenuItem struct {
	Text, Command string
	Enabled       bool
}
