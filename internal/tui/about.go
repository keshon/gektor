package tui

import (
	"runtime"
	"strings"
)

func (m *Model) openAbout() {
	m.chrome.Open("GEKTOR", []string{
		"Terminal coding agent written in Go.",
		"",
		m.chrome.Props.Styles.Gate.Render("Capabilities"),
		"Read, search and edit files, run commands and tests, and resume saved sessions.",
		"Plan before changes or work directly in Act mode; tool permissions stay visible.",
		"",
		m.chrome.Props.Styles.Gate.Render("Model backends"),
		"llama.cpp · KoboldCPP · OpenAI-compatible APIs",
		"Models and inference servers are supplied separately.",
		"",
		m.chrome.Props.Styles.Gate.Render("Repository"),
		"https://github.com/keshon/gektor",
		"Go " + strings.TrimPrefix(runtime.Version(), "go") + " · Bubble Tea · " + runtime.GOOS + "/" + runtime.GOARCH,
	})
}
