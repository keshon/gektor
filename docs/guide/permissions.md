# Review permissions and changes

Read the requested action and target before approving. Act mode enables changes under the current permission rules; switching to Act is not approval for every operation.

## Permissions

Open Permissions in Settings, or use `/permissions`, to read policy rules.
Use `/` to filter by decision, tool or pattern. Order numbers stay unchanged
when filtering: later matching rules take precedence. Details shows the full
pattern. This screen does not change rules or grant permission.

A permission request shows the operation and its target.
In Agent's permission prompt, `Y` allows once. `A` opens confirmation for
allowing the same tool and target for this run. `N` lets you add a redirect
note. Esc at the first prompt denies the request.
An explicit deny rule cannot be overridden here.

Shell commands, Git commands and external tools run with your account's
permissions. The workspace folder is not a sandbox for those programs.
See [security](../../SECURITY.md).
