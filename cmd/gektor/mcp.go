package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/keshon/gektor/internal/mcpmanager"
	"github.com/keshon/gektor/internal/userdirs"
)

func runMCP(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: gektor mcp list|import FILE|save FILE|delete ID|enable ID|disable ID|connect ID")
	}
	workspace, e := os.Getwd()
	if e != nil {
		return e
	}
	m, e := mcpmanager.New(userdirs.Sub("mcp"), workspace, userdirs.Sub("credentials"), nil)
	if e != nil {
		return e
	}
	defer m.Close()
	write := func(v any) error { return json.NewEncoder(os.Stdout).Encode(v) }
	if args[0] == "list" {
		if len(args) != 1 {
			return fmt.Errorf("list takes no arguments")
		}
		return write(m.List())
	}
	if len(args) != 2 {
		return fmt.Errorf("%s requires one argument", args[0])
	}
	switch args[0] {
	case "import", "import-replace":
		var raw []byte
		var e error
		if args[1] == "-" {
			raw, e = io.ReadAll(io.LimitReader(os.Stdin, (1<<20)+1))
		} else {
			raw, e = os.ReadFile(args[1])
		}
		if e != nil {
			return e
		}
		ids, e := m.Import(raw, args[0] == "import-replace")
		if e != nil {
			return e
		}
		return write(ids)
	case "save":
		raw, e := os.ReadFile(args[1])
		if e != nil {
			return e
		}
		var s mcpmanager.Server
		if json.Unmarshal(raw, &s) != nil {
			return fmt.Errorf("invalid server definition")
		}
		return m.Save(s)
	case "delete":
		return m.Delete(args[1])
	case "enable", "disable":
		s, e := m.Get(args[1])
		if e != nil {
			return e
		}
		s.Enabled = args[0] == "enable"
		return m.Save(s)
	case "connect":
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		n, e := m.Connect(ctx, args[1])
		if e != nil {
			return e
		}
		return write(map[string]any{"server": args[1], "tools": n, "status": "discovered", "connection": "closed when this command exits"})
	default:
		return fmt.Errorf("unknown MCP command %q", args[0])
	}
}
