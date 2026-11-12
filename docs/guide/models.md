# Connect a model

Agent needs a running model with reliable tool calling. Choose a local server, an OpenAI-compatible API or a ChatGPT account. Workflows without model steps need no connection.

## Model server

Start an inference server independently and wait for the model to load.
GEKTOR defaults to llama.cpp at `http://127.0.0.1:9931` in both CLI and the
first-run TUI profile. Configure other addresses under Model connection in Settings, or with
`-backend`; use `-backend-kind openai` for an OpenAI-compatible API.
KoboldCPP remains available through `-backend-kind kobold`.

For a ChatGPT account connection, run `gektor auth login` and select ChatGPT
in Model connection settings. See [authentication](../reference/cli.md#authentication-remote-providers).

GEKTOR does not install models or start, stop or supervise inference servers.
Readiness, server logs and recovery belong to your server setup. A lost
connection is reported as a request error.

## Local server or OpenAI-compatible API

In Settings > Model connection, choose the protocol and enter the server address.
For llama.cpp the default address is `http://127.0.0.1:9931`. For LM Studio or an
OpenAI-compatible API, choose `openai` and include `/v1` when the provider requires it.
Supply its actual model ID and context limit. Apply the settings before sending a task.

From the extracted application folder on Windows:

```powershell
.\gektor.exe -tui -backend-kind llama -backend http://127.0.0.1:9931
.\gektor.exe -tui -backend-kind openai -backend http://127.0.0.1:1234/v1 -model your-model -context-limit 16384
```

These are alternative connections. On Linux, replace `.\gektor.exe` with `./gektor`.
For a hosted service, set its API key in the environment; see
[authentication precedence](../reference/cli.md#authentication-remote-providers).
GEKTOR does not automatically load `.env`.

Choose a context budget your server and model can support. A local server's
reported window takes precedence over the fallback flag. Cached tokens still
occupy context. Use `/context` to inspect estimated costs and `/compact` while
idle to shorten a long conversation. See [sessions and context](sessions.md).

## ChatGPT account

The `chatgpt` backend uses browser sign-in and eligible ChatGPT plan usage;
it does not require an API key. Connect before starting a task:

```powershell
.\gektor.exe auth login
.\gektor.exe auth models
.\gektor.exe auth test gpt-6.1-sol
.\gektor.exe -tui -backend-kind chatgpt -model gpt-6.1-sol -context-limit 32768
```

Use an available model ID from `auth models`. The endpoint is fixed to
`https://api.openai.com/v1`; local-server URLs and API-key environment variables
do not apply. `auth status` shows the connection; `auth logout` revokes it.
`auth login --new` adds an account, `auth accounts` lists registrations and
`auth select <id>` selects one for subsequent requests. Switch accounts between
runs. Credentials are independent of workspace profiles and session history.

Credentials live under `~/.gektor/auth/chatgpt`. Windows encrypts the store with
user-bound DPAPI; Unix uses a private directory and owner-only file. Refreshes
are locked across processes. Login callbacks validate state, PKCE, the signed
ID token, nonce and the granted plan-usage scope. Tokens never enter profiles,
transcripts or debug output.

This backend uses Responses with streaming and client-maintained history.
GEKTOR still executes tools under its normal permissions. Opaque reasoning state
is retained with assistant messages for session resume and is replayed only to
this backend with the same model. Visible reasoning is a provider summary,
when supplied. Image input support comes from the account model catalog's
`input_modalities`. Models declaring `image` enable image-reading tools and
attachments. Missing metadata remains unknown; explicitly text-only models reject
images before inference.

Sampling and `max_output_tokens` are unsupported on this OAuth route.
Use `-reasoning-effort high` to select an effort level, or leave it unset for
the model default. Settings > Response settings lists levels from the selected
account's model catalog. Unsupported levels fail before inference.
Enable `-reasoning-summary` to request a visible thinking summary; Ctrl+G
cycles short, full and hidden thinking. This is a provider summary, not raw hidden reasoning.
Both controls are saved in profiles and currently apply only to ChatGPT.

Temperature and response-limit controls are hidden in Settings; `-max-tokens`
remains a local planning budget, not a server-enforced generation limit.
The configured context limit is also a local budget, not a measured model window;
the default without a profile is 32768. Plan-usage limits stop the run and refer
to ChatGPT Settings > Usage. Account eligibility and model access require a
successful inference request, not just a successful login or model listing.

See [OpenAI sign-in](https://developers.openai.com/siwc/token-sharing-open-source/sign-in)
and [route limitations](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations).
