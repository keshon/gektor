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

Open **Settings > Model connection** and choose a **Provider**.

- **llama.cpp** uses `http://127.0.0.1:9931` by default.
- **LM Studio** uses `http://127.0.0.1:1234/v1` by default. Start its local server and load a model first.
- **OpenRouter** uses `https://openrouter.ai/api/v1` by default.
- **OpenAI-compatible** takes the API root supplied by your service, including `/v1` where required.

For a provider that accepts bearer keys, choose **Authentication**:

- **None** sends no key. Use it for a server that allows unauthenticated requests.
- **Environment variable** reads the named variable from GEKTOR's environment.
- **Saved key** uses a key stored for this provider and server address. Press
  `F6`, paste the key into the masked field, and press `Ctrl+S`. If you change
  the address, enter the key for the new address.

Keys are stored under `~/.gektor/credentials/providers/` (Windows account
encryption or owner-only files on Unix).

Choose a model from the picker or type its ID, then apply the settings.
In **Response settings**, set **Context window** to `Auto` to use the selected
model's reported limit. GEKTOR reads it from llama.cpp and KoboldCpp server
probes or from the model catalog when the endpoint supplies it, as OpenRouter
does. Without metadata, GEKTOR labels its fallback as an estimate. Enter a
number to cap the working budget. The status menu shows the working budget
and the reported model limit when available.

From the extracted application folder on Windows:

```powershell
.\gektor.exe -tui -backend-kind llama -backend http://127.0.0.1:9931
.\gektor.exe -tui -backend-kind lmstudio -model your-model -context-limit 16384
```

These are alternative connections. On Linux, replace `.\gektor.exe` with `./gektor`.
To use an environment variable from the CLI, pass `-api-key-env NAME`. See
[authentication precedence](../reference/cli.md#authentication-remote-providers).
GEKTOR does not automatically load `.env`.

Cached tokens still occupy context. Use `/context` to inspect estimated costs and `/compact` while
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

Use a model ID from `auth models`. A successful `auth test` confirms that the
account can run that model. GEKTOR fixes the endpoint to
`https://api.openai.com/v1`. Run `auth status` to check the connection or
`auth logout` to revoke it.
`auth login --new` adds an account, `auth accounts` lists registrations and
`auth select <id>` selects one for subsequent requests. Switch accounts between
runs. Credentials are independent of workspace profiles and session history.

GEKTOR executes tools using its normal permissions. The account model catalog
determines which models accept images. See [account storage and binding](../reference/configuration.md#connection-ownership)
and [Responses replay](../../ARCHITECTURE.md#execution-and-limits).

Sampling and `max_output_tokens` are unsupported on this OAuth route.
Use `-reasoning-effort high` to select an effort level, or leave it unset for
the model default. Settings > Response settings lists levels from the selected
account's model catalog. Unsupported levels fail before inference.
Use `-reasoning-summary` to show the provider's thinking summary when available.
Press `Ctrl+G` to cycle through collapsed, expanded and hidden views.
Both controls are saved in profiles and currently apply only to ChatGPT.

Temperature and response-limit controls are hidden in Settings; `-max-tokens`
remains a local planning budget, not a server-enforced generation limit.
The configured context limit is also a local budget, not a measured model window;
the default without a profile is 32768. Plan-usage limits stop the run and refer
to ChatGPT Settings > Usage.

See [OpenAI sign-in](https://developers.openai.com/siwc/token-sharing-open-source/sign-in)
and [route limitations](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations).
