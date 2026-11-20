// Command workbench is an offline reading list using the shared terminal controls.
package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
)

func main() {
	err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	m := newModel()
	defer m.Close()
	_, err := tea.NewProgram(m).Run()
	return err
}
