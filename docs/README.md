# Documentation

Start here:

- [Quick start](../README.md): purpose, requirements and first launch.
- [Setup](bootstrap.md): external server setup, saved data and release contents.
- [Terminal guide](tui.md): using Agent and Workflows, with steps for everyday tasks.
- [Demos](../demos/README.md): launch complete tasks in fresh workspaces.
- [Workflow examples](../examples/workflows/README.md): recipes to copy, with their purpose and requirements.

Look up a specific task:

- [Workflow reference](workflows.md): operations, data mappings, run records, schedules and API.
- [CLI reference](cli.md): flags, backend examples, permissions and service commands.
- [Browser](browser.md): setup, persistent profiles, login and observation tools.
- [Extensions](extensions.md): capability packs, executable adapters and skill libraries.
- [Security](../SECURITY.md): permissions, workspace access and network behavior.

For developers:

- [RPC protocol](rpc.md): stdio requests, events, gates and client lifetime.
- [Architecture](../ARCHITECTURE.md): packages, loop, role budgets and frontends.
- [TUI design](tui-design.md): shared controls, layouts, interaction rules and migration plan.
- [TUI mockups](design/tui/README.md): editable FigCraft components, screens and palette studies.
- [Conventions](conventions.md): enforced checks and coding practices.
- [Cleanup plan](cleanup-plan.md): current work to simplify code and clarify ownership.
- [Eval guide](../eval/README.md): frozen probes and repeatable scoring.
- [Agent skills](../skills/README.md): task-workspace guidance for the local model.

The code is authoritative for behavior. Documentation checks cover registered
CLI flags, referenced identifiers and convention tags; they do not prove that
an example works or that every description remains semantically current.
