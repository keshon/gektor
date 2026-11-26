# Audit evidence

These probes describe revision `60365eeabd158bf32419d8b8b268542277eec898`.
The [audit report](../project-audit-2026-11-26.md) separates reproduced defects
from architectural observations and documentation changes proposed for later work.

Run the recovery assertions from any directory:

```powershell
pwsh -NoProfile -File docs/internal/audit-2026-11-26/harness-resume-repro.ps1
```

The relative script path above assumes the repository root. The script itself
resolves the repository from its own location. It leaves a temporary Go overlay
for inspection and does not change production source. Three failures and exit 1
are the expected result on the audited revision.

To repeat either Markdown probe, run from the repository root and copy the
chosen `.go.txt` source to a temporary `.go` file:

```powershell
$probePath = Join-Path ([System.IO.Path]::GetTempPath()) ('gektor-markdown-audit-' + [guid]::NewGuid().ToString('N') + '.go')
Copy-Item -LiteralPath docs/internal/audit-2026-11-26/markdown-latency.go.txt -Destination $probePath
go run $probePath
```

Use `markdown-probe.go.txt` instead for the small-fence and invalid-Rust cases.
Timings vary by machine, cache state and concurrent activity. Neither probe uses
a model or changes a session. Its output is an observation, not an acceptance
test that necessarily exits nonzero when the undesirable behavior occurs.

The public consumer command is `go run scripts/check-tui-package.go`.
Its failure and the successful complete-checkout counter-check are recorded in
`public-package-check.txt`. Gallery evidence records selected fixture test
results and byte equality after regeneration; it is not native terminal evidence.

`documentation-inventory.json` inventories tracked Markdown at the baseline.
Audience classification does not imply a complete semantic review of each file.
