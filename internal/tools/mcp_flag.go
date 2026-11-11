package tools

import (
	"fmt"
	"strings"
)

// ParseMCPFlag parses -mcp "name=cmd args...;name2=cmd2". Quoted arguments
// preserve spaces and semicolons; empty quoted arguments remain present.
func ParseMCPFlag(spec string) ([]string, []string, [][]string, error) {
	parser := mcpFlagParser{}
	var quote rune
	for _, character := range spec {
		if quote != 0 {
			if character == quote {
				quote = 0
				continue
			}
			parser.token.WriteRune(character)
			continue
		}

		switch character {
		case '\'', '"':
			quote = character
			parser.started = true
		case ';':
			err := parser.entry()
			if err != nil {
				return nil, nil, nil, err
			}
		case ' ', '\t', '\r', '\n':
			parser.flush()
		default:
			parser.token.WriteRune(character)
			parser.started = true
		}
	}

	if quote != 0 {
		return nil, nil, nil, fmt.Errorf("unterminated quote in MCP specification")
	}
	err := parser.entry()
	if err != nil {
		return nil, nil, nil, err
	}
	return parser.names, parser.commands, parser.args, nil
}

type mcpFlagParser struct {
	token    strings.Builder
	tokens   []string
	started  bool
	names    []string
	commands []string
	args     [][]string
}

func (p *mcpFlagParser) flush() {
	if !p.started {
		return
	}
	p.tokens = append(p.tokens, p.token.String())
	p.token.Reset()
	p.started = false
}

func (p *mcpFlagParser) entry() error {
	p.flush()
	if len(p.tokens) == 0 {
		return nil
	}

	name, command, assigned := strings.Cut(p.tokens[0], "=")
	if !assigned {
		command = name
	}
	if name == "" || command == "" {
		return fmt.Errorf("invalid MCP entry: expected name=command")
	}
	err := validateCapabilityID(name)
	if err != nil {
		return err
	}
	for _, previous := range p.names {
		if previous == name {
			return fmt.Errorf("duplicate MCP server %q", name)
		}
	}

	p.names = append(p.names, name)
	p.commands = append(p.commands, command)
	p.args = append(p.args, p.tokens[1:])
	p.tokens = nil
	return nil
}
