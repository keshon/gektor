# SSH commands and file transfers

Agent enables `native_ssh`; workflows use the same `ssh_exec`, `ssh_upload`
and `ssh_download` names. Connections are registered by the operator in
`%USERPROFILE%\.gektor\ssh.json` on Windows or `~/.gektor/ssh.json` elsewhere:

```json
{
  "staging": {
    "host": "staging.example.com",
    "port": 22,
    "user": "deploy",
    "known_hosts": "C:/Users/alice/.ssh/known_hosts",
    "private_key": "C:/Users/alice/.ssh/id_ed25519",
    "passphrase_env": "STAGING_KEY_PASSPHRASE"
  }
}
```

Use absolute filesystem paths; `~` and environment placeholders in paths are
not expanded. Omit `passphrase_env` for an unencrypted key. Environment-backed password authentication
uses `password_env` instead of `private_key` and `passphrase_env`. These fields
name environment variables; never put their values in this file, recipes or
tool arguments. Environment variables must be available to the GEKTOR process.
SSH and HTTP share environment lookup, while their connection settings stay separate.
Existing HTTP credential registration and origin checks are unchanged.

## Supplying credentials in the CLI and TUI

Use **Run > Tools > SSH** in Agent or Workflows to add, edit, delete and test
named connections. The ordinary editor saves key paths and credential references. On Windows,
a separate masked prompt can save passwords and key passphrases with account-bound
DPAPI encryption; it never writes their values into `ssh.json`. Alternatively, edit `ssh.json` yourself
or use the `ssh` management commands described below. `ssh_exec`, `ssh_upload`
and `ssh_download` remain Agent tools and workflow operations, not CLI subcommands.
GEKTOR does not load SSH secrets from `.env` automatically.

For password authentication, a complete connection entry looks like this:

```json
{
  "staging": {
    "host": "staging.example.com",
    "user": "deploy",
    "known_hosts": "C:/Users/alice/.ssh/known_hosts",
    "password_env": "GEKTOR_SSH_PASSWORD"
  }
}
```

In PowerShell, capture the password without putting its value in command history:

```powershell
$env:GEKTOR_SSH_PASSWORD = [System.Net.NetworkCredential]::new(
    '',
    (Read-Host 'SSH password' -AsSecureString)
).Password
```

Launch GEKTOR from that same terminal so it inherits the variable. Use your
normal model/profile flags alongside these commands:

```powershell
.\gektor.exe -tui
# Or run one Agent task:
.\gektor.exe "Use SSH connection staging to run uname -a"
```

In the TUI, ask Agent to use connection `staging`; it can enable `native_ssh`
when needed. Agent can call `ssh_connections` to list registered names and
endpoints, revisions and revocation status without connecting or resolving
credentials. It reads current metadata and does not expose credential references
or key paths. Every SSH operation requires `connection` and `connection_revision`;
copy the revision from discovery or `ssh inspect staging`.
Workflow workers must inherit the variable in their own process; setting it in
a TUI launch does not update a separately running worker. After GEKTOR exits,
remove the variable from the terminal if it is no longer needed:

```powershell
Remove-Item Env:GEKTOR_SSH_PASSWORD
```

For key authentication, use the `private_key` example above. Omit
`passphrase_env` for an unencrypted key. For an encrypted key, use the same
masked PowerShell prompt to set the variable named by `passphrase_env` before
launching. The key path points to an existing private key file; do not paste
key contents or secrets into chat. On other platforms, supply the referenced
variable through your shell or process supervisor before starting GEKTOR.

## Saved passwords and key passphrases

Choose **Authentication > Password** in the SSH editor. Saving a new connection
opens a separate masked prompt. On Windows, Ctrl+S saves with account-bound DPAPI
encryption and restarts; Esc cancels without writing the credential or profile.
Choose **Private key** for a key-file picker and optional passphrase. Existing
secrets have a **Saved — keep** state and an explicit **Change** action.

**Advanced settings** contains environment-variable sources and **Another
connection** for reusing a saved credential by connection name. The normal form
shows neither environment-variable names nor saved credential IDs. Port is always
visible and defaults to 22. The trusted hosts file is under Advanced and defaults
to the user's `~/.ssh/known_hosts`. Editing or hiding Advanced preserves existing
environment-based credentials unless the authentication choice is changed.

The resulting profile uses `password_ref` instead of `password_env`, or
`passphrase_ref` instead of `passphrase_env`. A profile must select exactly one
password source or a private key, and at most one passphrase source for that key.
An environment reference is always an environment reference; it is never
reinterpreted as a saved ID. Missing, deleted or revoked stored credentials fail
when connecting.
Saved password IDs cannot be used as key-passphrase IDs, or vice versa.

Encrypted values live under `~/.gektor/credentials/ssh/`; `ssh.json` contains only
opaque IDs. Multiple connections can use distinct IDs or share one of the same
purpose. To replace a saved value, choose **Change password** or **Change passphrase**
in the editor and enter the replacement at the masked prompt. Old values are retained because other connections or
worker snapshots may still use them. No automatic credential deletion or
session-only secret storage is provided.

This saved-secret feature requires Windows DPAPI. Other platforms retain key
files and environment references; no plaintext fallback is offered for SSH.
Workers resolve saved credentials under the same Windows account and credential
directory. Existing MCP credential IDs, files, HTTPS-origin and header binding
remain unchanged; its Unix owner-only storage is not encryption.

## Managing connections from the CLI

These commands need no model backend:

```powershell
.\gektor.exe ssh list
.\gektor.exe ssh save staging staging-profile.json
.\gektor.exe ssh check staging
.\gektor.exe ssh inspect staging
.\gektor.exe ssh revoke staging
.\gektor.exe ssh delete staging
```

The input to `ssh save` is one connection object (the value under `staging` in the
examples above), not the whole named map. Save adds or replaces that name while
preserving all other connections. `ssh list` shows reference-only configuration;
there are no raw-password flags. `ssh inspect` returns non-secret endpoint metadata,
a revision and revocation status. `ssh revoke` permanently disables that name,
even if its profile is deleted and recreated. Running services observe these
changes before their next connection; operations already started may finish.

`ssh check` has a 30-second limit and verifies the host key and authentication,
then closes. It opens no remote session, runs no command and transfers no file.
It does not test SFTP or hard-link support. Ctrl+C cancels it.

## Host verification and operation arguments

Populate `known_hosts` with host keys verified through a trusted channel before
connecting. Unknown or changed host keys fail; there is no trust-on-first-use or
verification bypass. SSH agent forwarding and interactive authentication are
not supported. Each command or transfer checks its pinned revision against current
profile metadata before connecting. Changing the endpoint, user, key path, trust
file path or credential reference invalidates the pin and its session approval.
Secret values and key/trust file contents are read when connecting and are not
part of the revision. Rotating a value at the same source keeps the pin unchanged.
A missing/invalid optional SSH config affects SSH calls only.

Command arguments:

```json
{"connection":"staging","connection_revision":"<revision from ssh inspect staging>","command":"uname -a","timeout_seconds":30}
```

The remote account's shell interprets `command`. Each call opens and closes a
fresh connection, so `cd`, shell variables and terminal state do not survive.
Use a single command such as `cd /srv/app && ./status` when needed. There is no
interactive terminal or stdin. Results contain `connection`, `stdout`, `stderr`
and `exit_code`. A nonzero exit is an operation error with captured output.
Each output stream is limited to 64 KiB; exceeding it closes the connection
and discards both captured streams to avoid exposing a truncated secret.
Cancellation or an interrupted command also discards both streams; a normal
nonzero exit retains redacted output. Credential matching includes substrings
and JSON-escaped values, so text containing a credential is masked even inside
a larger word or URL.

Transfer arguments (the same shape for upload and download):

```json
{"connection":"staging","connection_revision":"<revision from ssh inspect staging>","local_path":"dist/app.zip","remote_path":"/srv/releases/app.zip","max_bytes":16777216,"timeout_seconds":60}
```

Transfers use SFTP, accept one regular file up to 16 MiB, and return paths,
size and SHA-256 of transferred bytes. `local_path` stays inside the workspace;
`remote_path` must be an absolute POSIX path. Remote links follow the server's
filesystem semantics. Parent directories must already exist. Existing destinations,
including links, are refused. Local downloads publish complete content using
a hard link, so the destination filesystem must support hard links.
Uploads use `hardlink@openssh.com` for the same guarantee and fail explicitly
when the server does not support it. A broken connection may leave a remote
`.gektor-*` temporary file in the destination folder; remove it after checking
the transfer outcome. No permissions or timestamps are copied.

All three operations ask for approval under the default policy and are unavailable
in Plan/read-only execution. Remote permission resources include the configured
user, host, port and revision (`ssh://deploy@staging.example.com:22/...#revision`); command resources
also include the command. Transfers authorize both remote and local resources.
Additionally, uploads check the existing `read_file` policy for the local source,
and downloads check `write_file` for the destination before connecting. Sensitive
file restrictions therefore apply even when an SSH operation itself is allowed.

`timeout_seconds` defaults to 30 and accepts 1–600. There are no automatic retries;
workflow retry settings are rejected for SSH nodes. Closing an SSH connection
does not guarantee a remote command stopped or a completed upload was rolled back.
After timeout/disconnection, verify remote state before repeating an operation.
Treat remote output as untrusted content.
