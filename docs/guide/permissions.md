# Review permissions and changes

Read the requested action and target before approving. Act mode enables changes under the current permission rules; switching to Act is not approval for every operation.

## Permissions

Open Permissions in Settings, or use `/permissions`, to read policy rules.
Use `/` to filter by decision, tool or pattern. Order numbers stay unchanged
when filtering: later matching rules take precedence. Details shows the full
pattern. This screen does not change rules or grant permission.

A permission request shows the operation and its target.
In Agent's permission prompt, `Y` allows once. `A` opens confirmation for
allowing the same tool and target for this session, including follow-up messages.
`N` lets you add a redirect note. Esc at the first prompt denies the request.
An explicit deny rule cannot be overridden here.

For SSH commands, the target is the connection endpoint (user, host and port),
so another command on that connection does not prompt again. Other targets stay
exact: a file path, transfer path, URL or local command. Different tools and
targets need separate approval. The confirmation shows the scope. CLI and RPC
use the same scope; `a` and `always` remain accepted answers.

In Agent’s top **Run** menu, choose **Ask permission** or **Full access** while
idle. Full access approves policy Ask decisions automatically for the open
session; a FULL ACCESS indicator stays visible. Explicit deny rules and Plan
mode restrictions still apply. Returning to Ask permission clears remembered
approvals. New or reopened sessions start in Ask permission. Approvals are
not saved to disk.

Workflow’s Run menu has the same choices for the open recipe. Its permission
form offers Allow once and Allow for this session. Opening or creating a recipe
clears approvals. These choices do not grant access to scheduled background jobs.

Shell commands, Git commands and external tools run with your account's
permissions. The workspace folder is not a sandbox for those programs.
See [security](../../SECURITY.md).
