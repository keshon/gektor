package screen

// Menu names one group of application commands. Screens supply their local
// groups; the terminal host adds shared navigation, settings, and help.
type Menu struct {
	ID, Label string
	Items     []MenuItem
}

// MenuItem describes a command's current availability. An empty item separates
// adjacent groups; rendering and dispatch belong to the terminal host.
type MenuItem struct {
	Text, Command string
	Shortcut      string
	Checked       bool
	Enabled       bool
}

// Badge is a selectable label in a screen's status header.
type Badge struct {
	ID, Label  string
	Start, End int
}

// ActionCell is the measured toolbar region occupied by one action.
type ActionCell struct {
	Action          Action
	Row, Start, End int
}
