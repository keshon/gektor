package mcpmanager

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/keshon/gektor/internal/credentials"
)

// BindingStatus omits credential references and command arguments.
type BindingStatus struct {
	ID        string `json:"id"`
	Revision  string `json:"revision"`
	Transport string `json:"transport"`
	Endpoint  string `json:"endpoint"`
	Enabled   bool   `json:"enabled"`
	Revoked   bool   `json:"revoked"`
}

// savedBinding retains no manager state or secrets and can be checked concurrently.
type savedBinding struct {
	path     string
	revision string
}

func serverRevision(server Server) (string, error) {
	// Exposure switches do not change the endpoint or invalidate a retained catalog.
	pack := server.Pack()
	raw, err := json.Marshal(pack)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum), nil
}

func inspectBinding(path string) (BindingStatus, error) {
	file, err := os.Open(path)
	if err != nil {
		return BindingStatus{}, fmt.Errorf("MCP binding unavailable: %w", err)
	}
	defer file.Close() // Read-only configuration; closing cannot change the binding.
	raw, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil {
		return BindingStatus{}, err
	}
	if len(raw) > 1<<20 {
		return BindingStatus{}, fmt.Errorf("MCP definition exceeds 1 MiB")
	}

	var server Server
	err = decode(raw, &server)
	if err != nil {
		return BindingStatus{}, fmt.Errorf("invalid MCP binding")
	}
	if !validID.MatchString(server.ID) || filepath.Base(path) != server.ID+".json" {
		return BindingStatus{}, fmt.Errorf("MCP filename and ID disagree")
	}

	revision, err := serverRevision(server)
	if err != nil {
		return BindingStatus{}, err
	}

	revocations := credentials.Revocations{Directory: filepath.Join(filepath.Dir(path), "revoked")}
	revoked, err := revocations.Revoked(server.ID)
	if err != nil {
		return BindingStatus{}, err
	}

	status := BindingStatus{
		ID:        server.ID,
		Revision:  revision,
		Transport: "http",
		Endpoint:  server.URL,
		Enabled:   server.Enabled,
		Revoked:   revoked,
	}
	if len(server.Command) > 0 {
		status.Transport, status.Endpoint = "stdio", server.Command[0]
	}
	return status, nil
}

func (b savedBinding) Check() error {
	current, err := inspectBinding(b.path)
	if err != nil {
		return err
	}
	if current.Revoked {
		return fmt.Errorf("MCP binding is revoked")
	}
	if !current.Enabled {
		return fmt.Errorf("MCP server is disabled")
	}
	if current.Revision != b.revision {
		return fmt.Errorf("MCP binding changed; reload its configuration before reconnecting")
	}
	return nil
}

func (b savedBinding) Target() string {
	name := filepath.Base(b.path)
	return "mcp:" + name[:len(name)-len(".json")] + "#" + b.revision
}

// Inspect reads current saved metadata without opening a session or resolving secrets.
func (m *Manager) Inspect(id string) (BindingStatus, error) {
	if !validID.MatchString(id) {
		return BindingStatus{}, fmt.Errorf("invalid MCP server ID")
	}
	return inspectBinding(filepath.Join(m.directory, id+".json"))
}

// Revoke permanently disables the saved name, including retained sessions.
func (m *Manager) Revoke(id string) error {
	_, err := m.Inspect(id)
	if err != nil {
		return err
	}
	revocations := credentials.Revocations{Directory: filepath.Join(m.directory, "revoked")}
	return revocations.Revoke(id)
}
