@echo off
setlocal
cd /d "%~dp0"

REM OpenAI-compatible stdio server launcher.
REM Credentials live in .env (gitignored); see .env.example.
REM
REM   run-server-openai.cmd
REM   set OPENAI_MODEL=some/model && run-server-openai.cmd

REM --- load .env ---
set "ENV_TMP=%TEMP%\gektor-env-%RANDOM%.tmp"
if exist .env (
    powershell -NoProfile -ExecutionPolicy Bypass -File scripts\set-env.ps1 .env > "%ENV_TMP%" 2>nul
    if exist "%ENV_TMP%" (
        for /f "delims=" %%a in (%ENV_TMP%) do set "%%a"
        del "%ENV_TMP%"
    )
)

REM --- defaults ---
REM Endpoint settings are OPENAI_* because they describe this backend, the
REM same way LLAMA_* and KOBOLD_* describe theirs. Only GEKTOR_API_KEY keeps
REM the Gektor prefix: internal/llm.APIKeyFromEnv reads it first, ahead of the
REM per-provider keys, as the generic name.
if "%OPENAI_BACKEND%"=="" set OPENAI_BACKEND=https://openrouter.ai/api/v1
if "%OPENAI_MODEL%"=="" set OPENAI_MODEL=cohere/north-mini-code:free
if "%OPENAI_WORKSPACE%"=="" set OPENAI_WORKSPACE=.

REM --- required API key ---
if "%GEKTOR_API_KEY%"=="" set "GEKTOR_API_KEY=%TARS_API_KEY%"
if "%GEKTOR_API_KEY%"=="" (
    echo ERROR: GEKTOR_API_KEY is not set.
    echo Copy .env.example to .env and fill in your key.
    exit /b 1
)

call scripts\ensure-build.cmd
if errorlevel 1 exit /b %errorlevel%

REM --- export keys so the Go client's APIKeyFromEnv() can pick them up ---
set OPENROUTER_API_KEY=%GEKTOR_API_KEY%

REM --- launch ---
echo Starting GEKTOR stdio server (backend: %OPENAI_BACKEND%, model: %OPENAI_MODEL%)...
.\gektor.exe -serve -backend-kind openai -backend %OPENAI_BACKEND% -model %OPENAI_MODEL% -workspace %OPENAI_WORKSPACE% -yes
exit /b %errorlevel%