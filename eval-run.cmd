@echo off
setlocal
cd /d "%~dp0"

REM Run the eval suite against a backend that is already running.
REM
REM   eval-run.cmd                       every probe, once
REM   eval-run.cmd -tier smoke           quick subset
REM   eval-run.cmd -suite core -runs 3   frozen seven-scenario baseline
REM   eval-run.cmd -only 05 -runs 3      one probe, three runs
REM   eval-run.cmd -backend-kind kobold  target koboldcpp instead
REM   eval-run.cmd -require-model qwen   refuse other weights
REM
REM With no -backend/-backend-kind it targets the llama server described by
REM .env (LLAMA_HOST, LLAMA_PORT, LLAMA_CTX_SIZE), the same .env
REM run-server-llama.cmd starts from. Previously it fell through to cmd/eval's
REM koboldcpp default and every run had to repeat two flags by hand. Those
REM defaults are prepended, so passing -backend or -backend-kind overrides
REM them without any special handling here.
REM
REM All other flags pass through to cmd/eval; see eval\README.md. Results
REM land in eval\results\<timestamp>\ (gitignored).

set "ENV_TMP=%TEMP%\gektor-eval-env-%RANDOM%.tmp"
if exist .env (
    powershell -NoProfile -ExecutionPolicy Bypass -File scripts\set-env.ps1 .env > "%ENV_TMP%" 2>nul
    if exist "%ENV_TMP%" (
        for /f "delims=" %%a in (%ENV_TMP%) do set "%%a"
        del "%ENV_TMP%"
    )
)

REM Defaults go FIRST so a flag passed on the command line wins: Go's flag
REM package takes the last occurrence, so there is no need to inspect %* and
REM decide whether the caller already supplied one.
if "%LLAMA_HOST%"=="" set "LLAMA_HOST=127.0.0.1"
if "%LLAMA_PORT%"=="" set "LLAMA_PORT=8080"
set "EVAL_EXTRA=-backend-kind llama -backend http://%LLAMA_HOST%:%LLAMA_PORT%"
if not "%LLAMA_CTX_SIZE%"=="" set "EVAL_EXTRA=%EVAL_EXTRA% -context-limit %LLAMA_CTX_SIZE%"

go run ./cmd/eval %EVAL_EXTRA% %*
exit /b %errorlevel%
