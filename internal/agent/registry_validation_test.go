package agent

import "testing"

func TestInvalidInitialRegistryFailsBeforeModel(t *testing.T) {
	for _, list := range [][]Tool{{echoToolStub{}, echoToolStub{}}, {echoToolStub{name: "invalid.name"}}, {nil}} {
		client := &stubClient{}
		reg := NewRegistry(list...)
		if reg.Err() == nil || len(reg.Names()) != 0 {
			t.Fatal("invalid catalog partially installed")
		}
		a := New(Config{Client: client, Tools: reg})
		if _, err := a.Run(t.Context(), "task"); err == nil || client.calls != 0 {
			t.Fatal("invalid registry reached model", err, client.calls)
		}
	}
}

type readOnlyEcho struct{ echoToolStub }

func (readOnlyEcho) ReadOnly() bool { return true }

func TestReadOnlyExtensionRequiresRoleOptIn(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		a := New(Config{Tools: NewRegistry(readOnlyEcho{}), ReadOnly: []string{}, AllowReadOnlyExtensions: enabled})
		if a.mayRun("echo") != enabled {
			t.Fatal("extension bypassed exact role allowlist", enabled)
		}
	}
}
