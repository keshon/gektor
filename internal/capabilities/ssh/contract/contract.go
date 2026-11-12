// Package contract shares SSH schemas, validation and execution across frontends.
package contract

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/keshon/gektor/internal/capabilities/ssh"
	"github.com/keshon/gektor/internal/workflow"
)

type Operation struct {
	Service       *ssh.Service
	Kind          string
	AuthorizeFile ssh.FileAuthorizer
}

type specification struct {
	name, description, input, output string
}

const execInput = `{
  "type": "object",
  "properties": {
    "connection": {"type":"string","minLength":1},
    "command": {"type":"string","minLength":1,"maxLength":32768},
    "timeout_seconds": {"type":"integer","minimum":1,"maximum":600}
  },
  "required": ["connection","command"],
  "additionalProperties": false
}`
const transferInput = `{
  "type": "object",
  "properties": {
    "connection": {"type":"string","minLength":1},
    "local_path": {"type":"string","minLength":1},
    "remote_path": {"type":"string","minLength":1},
    "max_bytes": {"type":"integer","minimum":1,"maximum":16777216},
    "timeout_seconds": {"type":"integer","minimum":1,"maximum":600}
  },
  "required": ["connection","local_path","remote_path"],
  "additionalProperties": false
}`
const execOutput = `{
  "type": "object",
  "properties": {
    "connection": {"type":"string"},
    "stdout": {"type":"string"},
    "stderr": {"type":"string"},
    "exit_code": {"type":"integer"}
  },
  "required": ["connection","stdout","stderr","exit_code"],
  "additionalProperties": false
}`
const transferOutput = `{
  "type": "object",
  "properties": {
    "connection": {"type":"string"},
    "local_path": {"type":"string"},
    "remote_path": {"type":"string"},
    "size": {"type":"integer"},
    "sha256": {"type":"string"}
  },
  "required": ["connection","local_path","remote_path","size","sha256"],
  "additionalProperties": false
}`

func specifications() []specification {
	return []specification{
		{
			name:        "ssh_exec",
			description: "Run a remote shell command using a registered SSH connection. Fresh verified connection per call; no interactive terminal, retained working directory or retries. Timeout defaults to 30 seconds (max 600). stdout/stderr are limited to 64 KiB each; overflow discards output. Cancellation cannot guarantee the remote command stopped; verify its outcome before retrying.",
			input:       execInput,
			output:      execOutput,
		},
		{
			name:        "ssh_upload",
			description: "Upload one regular workspace file to an absolute POSIX remote_path via SFTP. Registered SSH connection; local read_file permission applies independently. Up to 16 MiB; no overwrites, directory creation or retries. Parent directories must exist. Server must support hardlink@openssh.com for safe publication. Interrupted uploads may leave a remote .gektor-* temporary file.",
			input:       transferInput,
			output:      transferOutput,
		},
		{
			name:        "ssh_download",
			description: "Download one regular remote file via SFTP to a new workspace local_path. Registered SSH connection; local write_file permission applies independently. Up to 16 MiB; no overwrites, directory creation or retries. Parent directories must exist. Publishes complete content; returns size and SHA-256.",
			input:       transferInput,
			output:      transferOutput,
		},
	}
}

func Names() []string {
	var names []string
	for _, spec := range specifications() {
		names = append(names, spec.name)
	}
	return names
}

func (o Operation) spec() specification {
	for _, spec := range specifications() {
		if spec.name == o.Kind {
			return spec
		}
	}
	return specification{}
}

func (o Operation) Name() string                  { return o.Kind }
func (o Operation) Description() string           { return o.spec().description }
func (o Operation) Schema() json.RawMessage       { return json.RawMessage(o.spec().input) }
func (o Operation) OutputSchema() json.RawMessage { return json.RawMessage(o.spec().output) }
func (o Operation) ReadOnly() bool                { return false }
func (o Operation) RetrySafe() bool               { return false }

func (o Operation) parse(args json.RawMessage) (ssh.ExecRequest, ssh.TransferRequest, error) {
	var command ssh.ExecRequest
	var transfer ssh.TransferRequest
	if o.spec().name == "" {
		return command, transfer, fmt.Errorf("unknown SSH operation %q", o.Kind)
	}
	err := workflow.ValidateJSON(args, o.Schema())
	if err != nil {
		return command, transfer, err
	}
	if o.Kind == "ssh_exec" {
		err = json.Unmarshal(args, &command)
		if err == nil {
			command, err = o.Service.ValidateExec(command)
		}
	} else {
		err = json.Unmarshal(args, &transfer)
		if err == nil {
			transfer, err = o.Service.ValidateTransfer(transfer)
		}
	}
	return command, transfer, err
}

func (o Operation) Validate(args json.RawMessage) error {
	_, _, err := o.parse(args)
	return err
}

func (o Operation) Resources(args json.RawMessage) ([]string, error) {
	command, transfer, err := o.parse(args)
	if err != nil {
		return nil, err
	}
	connection, remotePath := transfer.Connection, transfer.RemotePath
	if o.Kind == "ssh_exec" {
		connection = command.Connection
	}
	resource, err := o.Service.Resource(connection, remotePath)
	if err != nil {
		return nil, err
	}
	if o.Kind == "ssh_exec" {
		return []string{resource + " " + command.Command}, nil
	}
	return []string{resource, transfer.LocalPath}, nil
}

func (o Operation) Run(ctx context.Context, args json.RawMessage) (string, error) {
	command, transfer, err := o.parse(args)
	if err != nil {
		return "", err
	}
	var result any
	if o.Kind == "ssh_exec" {
		result, err = o.Service.Exec(ctx, command)
	} else {
		direction := ssh.Download
		if o.Kind == "ssh_upload" {
			direction = ssh.Upload
		}
		result, err = o.Service.Transfer(ctx, transfer, direction, o.AuthorizeFile)
	}
	encoded, encodeErr := json.Marshal(result)
	if encodeErr != nil {
		return "", encodeErr
	}
	return string(encoded), err
}

func (o Operation) ValidateTemplate(raw json.RawMessage) error {
	return workflow.ValidateLiteralPaths(raw, []string{"local_path"}, func(filename string) error {
		if o.Service == nil || o.Service.WS == nil {
			return fmt.Errorf("workspace unavailable")
		}
		_, err := o.Service.WS.Relative(filename)
		return err
	})
}
