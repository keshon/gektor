package agent

import (
	"encoding/json"
)

// mayRun reports whether this role is allowed to execute a tool. Unrestricted
// roles allow every name the registry knows.
func (a *Agent) mayRun(name string) bool {
	if a.allowed == nil || a.allowed[name] {
		return true
	}
	if !a.cfg.AllowReadOnlyExtensions {
		return false
	}
	if tool, ok := a.cfg.Tools.lookup(name); ok {
		if extension, ok := tool.(interface{ ReadOnly() bool }); ok {
			return extension.ReadOnly()
		}
	}
	return false
}

// Argument-sensitive admission is reserved for locally implemented tools. It
// supplements the Plan allowlist; authorization still runs for every call.
func (a *Agent) mayRunCall(name string, args json.RawMessage) bool {
	if a.mayRun(name) {
		return true
	}
	if !a.cfg.AllowReadOnlyExtensions {
		return false
	}
	if tool, ok := a.cfg.Tools.lookup(name); ok {
		if scoped, ok := tool.(interface{ ReadOnlyCall(json.RawMessage) bool }); ok {
			return scoped.ReadOnlyCall(args)
		}
	}
	return false
}
