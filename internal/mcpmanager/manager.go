// Package mcpmanager owns saved MCP definitions and their launch-scoped connections.
package mcpmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/keshon/gektor/internal/mcpmanager/credentials"
	"github.com/keshon/gektor/internal/tools"
)

type Server struct {
	ID          string            `json:"id"`
	Description string            `json:"description"`
	URL         string            `json:"url,omitempty"`
	Command     []string          `json:"command,omitempty"`
	HeaderRefs  map[string]string `json:"header_refs,omitempty"`
	EnvRefs     map[string]string `json:"env_refs,omitempty"`
	Enabled     bool              `json:"enabled"`
	Startup     bool              `json:"startup,omitempty"`
}
type Status struct {
	Server
	State     string   `json:"state"`
	ToolNames []string `json:"tool_names,omitempty"`
	Connected bool     `json:"connected"`
	Tools     int      `json:"tools"`
	Error     string   `json:"error,omitempty"`
}
type Manager struct {
	mu                   sync.Mutex
	directory, workspace string
	credentials          credentials.Store
	servers              map[string]Server
	sources              map[string]*tools.Capability
	errors               map[string]string
	reserved             map[string]bool
}

var validID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

func New(directory, workspace, credentialDir string, reserved []string) (*Manager, error) {
	m := &Manager{directory: directory, workspace: workspace, credentials: credentials.Store{Directory: credentialDir}, servers: map[string]Server{}, sources: map[string]*tools.Capability{}, errors: map[string]string{}, reserved: map[string]bool{}}
	for _, id := range reserved {
		m.reserved[id] = true
	}
	entries, e := os.ReadDir(directory)
	if os.IsNotExist(e) {
		return m, nil
	}
	if e != nil {
		return nil, e
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, e := os.ReadFile(filepath.Join(directory, entry.Name()))
		if e != nil {
			return nil, e
		}
		var s Server
		if e = decode(raw, &s); e != nil {
			return nil, fmt.Errorf("invalid MCP server file %s", entry.Name())
		}
		if entry.Name() != s.ID+".json" {
			return nil, fmt.Errorf("MCP filename and ID disagree")
		}
		if e = m.validate(s); e != nil {
			return nil, e
		}
		m.install(s)
	}
	return m, nil
}
func (s Server) Pack() tools.CapabilityPack {
	return tools.CapabilityPack{ID: s.ID, Version: "1", Description: s.Description, Type: "mcp", URL: s.URL, Command: s.Command, HeaderRefs: s.HeaderRefs, EnvRefs: s.EnvRefs}
}
func (m *Manager) validate(s Server) error {
	for id := range m.servers {
		if id != s.ID && strings.EqualFold(id, s.ID) {
			return fmt.Errorf("MCP IDs must differ ignoring case")
		}
	}
	if !validID.MatchString(s.ID) || strings.HasPrefix(s.ID, "native_") || m.reserved[s.ID] {
		return fmt.Errorf("invalid or already configured MCP ID %q", s.ID)
	}
	if s.Description == "" {
		return fmt.Errorf("description is required")
	}
	_, e := tools.ValidateCapabilityConfig(tools.CapabilityConfig{Version: 1, Packs: []tools.CapabilityPack{s.Pack()}}, m.workspace)
	return e
}
func (m *Manager) install(s Server) {
	if old := m.sources[s.ID]; old != nil {
		old.Disconnect()
	}
	pack := s.Pack()
	pack.CredentialDirectory = m.credentials.Directory
	list, _ := tools.BuildCapabilities(tools.CapabilityConfig{Packs: []tools.CapabilityPack{pack}}, m.workspace)
	list[0].Eager = s.Startup
	m.servers[s.ID] = s
	m.sources[s.ID] = list[0]
	delete(m.errors, s.ID)
}
func (m *Manager) Save(s Server) error {
	s = s.clone()
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.validate(s); e != nil {
		return e
	}
	if e := os.MkdirAll(m.directory, 0700); e != nil {
		return e
	}
	raw, e := json.MarshalIndent(s, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(m.directory, ".mcp-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(raw)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if e = os.Rename(name, filepath.Join(m.directory, s.ID+".json")); e != nil {
		return e
	}
	old := m.servers[s.ID]
	m.install(s)
	m.removeUnusedCredentials(old)
	return nil
}
func (m *Manager) List() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Status{}
	for id, s := range m.servers {
		state, count, lastError := m.sources[id].ConnectionStatus()
		if lastError == "" {
			lastError = m.errors[id]
		}
		out = append(out, Status{Server: s.clone(), State: state, Connected: state == "connected", Tools: count, Error: lastError, ToolNames: m.sources[id].ToolNames()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (m *Manager) Get(id string) (Server, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.servers[id]
	if !ok {
		return s, fmt.Errorf("unknown MCP server %q", id)
	}
	return s.clone(), nil
}
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.servers[id]; !ok {
		return fmt.Errorf("unknown MCP server")
	}
	if e := os.Remove(filepath.Join(m.directory, id+".json")); e != nil {
		return e
	}
	m.sources[id].Disconnect()
	old := m.servers[id]
	delete(m.sources, id)
	delete(m.servers, id)
	m.removeUnusedCredentials(old)
	delete(m.errors, id)
	return nil
}
func (m *Manager) Sources() []*tools.Capability {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := []string{}
	for id, s := range m.servers {
		if s.Enabled {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	out := []*tools.Capability{}
	for _, id := range ids {
		out = append(out, m.sources[id])
	}
	return out
}
func (m *Manager) Connect(ctx context.Context, id string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.servers[id]
	if !ok {
		return 0, fmt.Errorf("unknown MCP server")
	}
	if !s.Enabled {
		return 0, fmt.Errorf("enable the server before connecting")
	}
	n, e := m.sources[id].Connect(ctx)
	if e != nil {
		m.errors[id] = e.Error()
		m.sources[id].Eager = false
	} else {
		delete(m.errors, id)
		m.sources[id].Eager = true
	}
	return n, e
}
func (m *Manager) Disconnect(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	source := m.sources[id]
	if source == nil {
		return fmt.Errorf("unknown MCP server")
	}
	source.Disconnect()
	source.Eager = false
	delete(m.errors, id)
	return nil
}
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sources {
		s.Disconnect()
	}
}

// Import accepts the common MCP configuration without executing any server.
// Existing IDs are rejected so importing cannot silently replace a connection.
func (m *Manager) Import(raw []byte, replace ...bool) ([]string, error) {
	if len(raw) > 1<<20 {
		return nil, fmt.Errorf("MCP import exceeds 1 MiB")
	}
	var doc struct {
		Servers map[string]struct {
			URL     string            `json:"url"`
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Headers map[string]string `json:"headers"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if decode(raw, &doc) != nil || len(doc.Servers) == 0 {
		return nil, fmt.Errorf("expected an mcpServers object")
	}
	var pending []Server
	seen := map[string]bool{}
	for id, v := range doc.Servers {
		folded := strings.ToLower(id)
		if seen[folded] {
			return nil, fmt.Errorf("MCP IDs must differ ignoring case")
		}
		seen[folded] = true
		if _, e := m.Get(id); e == nil && !(len(replace) > 0 && replace[0]) {
			return nil, fmt.Errorf("MCP server %s already exists; edit it instead", id)
		}
		if len(v.Env) > 0 {
			return nil, fmt.Errorf("use env_refs for subprocess credentials; inline env import is not supported")
		}
		s := Server{ID: id, Description: id + " MCP service", URL: v.URL, Enabled: true}
		if v.Command != "" {
			s.Command = append([]string{v.Command}, v.Args...)
		}
		if e := m.validate(s); e != nil {
			return nil, e
		}
		if len(v.Headers) > 0 {
			if _, e := credentials.Origin(v.URL); e != nil {
				return nil, e
			}
			s.HeaderRefs = map[string]string{}
			for name, value := range v.Headers {
				if value == "" || len(value) > 16384 || strings.ContainsAny(value, "\r\n") {
					return nil, fmt.Errorf("invalid HTTP header value")
				}
				for other := range s.HeaderRefs {
					if strings.EqualFold(other, name) {
						return nil, fmt.Errorf("duplicate HTTP header")
					}
				}
				s.HeaderRefs[name] = "pending"
			}
			if e := m.validate(s); e != nil {
				return nil, e
			}
		}
		pending = append(pending, s)
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].ID < pending[j].ID })
	ids := []string{}
	for _, s := range pending {
		refs := []string{}
		for name, value := range doc.Servers[s.ID].Headers {
			ref, e := m.credentials.Put(s.URL, name, value)
			if e != nil {
				for _, r := range refs {
					m.credentials.Delete(r)
				}
				return ids, e
			}
			refs = append(refs, ref)
			s.HeaderRefs[name] = ref
		}
		if e := m.Save(s); e != nil {
			for _, r := range refs {
				m.credentials.Delete(r)
			}
			return ids, e
		}
		ids = append(ids, s.ID)
	}
	return ids, nil
}

func (m *Manager) removeUnusedCredentials(old Server) {
	for _, ref := range old.HeaderRefs {
		used := false
		for _, s := range m.servers {
			for _, current := range s.HeaderRefs {
				if current == ref {
					used = true
				}
			}
		}
		if !used {
			_ = m.credentials.Delete(ref)
		}
	}
}

func decode(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("expected one JSON document")
	}
	return nil
}

func (s Server) clone() Server {
	s.Command = slices.Clone(s.Command)
	s.HeaderRefs = maps.Clone(s.HeaderRefs)
	s.EnvRefs = maps.Clone(s.EnvRefs)
	return s
}
