# HTTP artifact download

Copy this folder, copy `inputs.example.json` to `inputs.json`, and replace its
example URL with a public HTTP(S) file URL you want to download. The supplied URL
is a placeholder. No model is required.

From a built checkout or extracted application folder, using PowerShell:

```powershell
.\gektor.exe workflow validate -workspace examples/workflows/http-artifact -inputs inputs.json recipe.json
.\gektor.exe workflow run -workspace examples/workflows/http-artifact -inputs inputs.json recipe.json
.\gektor.exe workflow artifacts -workspace examples/workflows/http-artifact <run-id>
.\gektor.exe workflow export-artifact -workspace examples/workflows/http-artifact <run-id> <artifact-id> download.bin
```

Use the copied folder as `-workspace`. On Linux, use `./gektor` and equivalent
shell syntax. Approvals cover the request URL, artifact name and each redirect
destination. The response streams into run storage with a 16 MiB limit; it is not
returned as inline text. Exporting explicitly creates the workspace file.

A downstream HTTP step can upload the bytes with `body_format: "artifact"` and
`body_artifact: {"$step":"download","pointer":"/artifact/id"}`. The artifact stays
owned by this run and survives a review pause. See the
[HTTP reference](../../../docs/reference/recipes.md#http-requests) for formats,
credential pins, response headers and redirect policy.
