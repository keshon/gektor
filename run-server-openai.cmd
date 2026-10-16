@echo off
setlocal
cd /d "%~dp0"

REM OpenAI-compatible stdio server launcher.
REM Credentials live in .env (gitignored); see .env.example.
REM
REM   run-server-openai.cmd
REM   set TARS_MODEL=some/model && run-server-openai.cmd

REM --- load .env ---
set "ENV_TMP=%TEMP%\tars-env-%RANDOM%.tmp"
if exist .env (
    powershell -NoProfile -ExecutionPolicy Bypass -File scripts\set-env.ps1 .env > "%ENV_TMP%" 2>nul
    if exist "%ENV_TMP%" (
        for /f "delims=" %%a in (%ENV_TMP%) do set "%%a"
        del "%ENV_TMP%"
    )
)

REM --- defaults ---
if "%TARS_BACKEND%"=="" set TARS_BACKEND=https://openrouter.ai/api/v1
if "%TARS_MODEL%"=="" set TARS_MODEL=cohere/north-mini-code:free
if "%TARS_WORKSPACE%"=="" set TARS_WORKSPACE=.

REM --- required API key ---
if "%TARS_API_KEY%"=="" (
    echo ERROR: TARS_API_KEY is not set.
    echo Copy .env.example to .env and fill in your key.
    exit /b 1
)

REM --- where go ---
where go >nul 2>nul
if errorlevel 1 (
    echo ERROR: Go was not found. Install Go 1.26.6 or newer first.
    exit /b 1
)

REM --- export keys so the Go client's APIKeyFromEnv() can pick them up ---
set TARS_API_KEY=%TARS_API_KEY%
set OPENROUTER_API_KEY=%TARS_API_KEY%

REM --- launch ---
echo Starting TARS stdio server (backend: %TARS_BACKEND%, model: %TARS_MODEL%)...
go run ./cmd/agent -serve -backend-kind openai -backend %TARS_BACKEND% -model %TARS_MODEL% -workspace %TARS_WORKSPACE% -yes
exit /b %errorlevel%