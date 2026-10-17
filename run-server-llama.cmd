@echo off
setlocal
cd /d "%~dp0"

REM llama.cpp server launcher (default backend).
REM
REM   run-server-llama.cmd
REM   set LLAMA_PORT=9000 && run-server-llama.cmd

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
if "%LLAMA_PORT%"=="" set LLAMA_PORT=8080
if "%LLAMA_HOST%"=="" set LLAMA_HOST=127.0.0.1
if "%LLAMA_CTX_SIZE%"=="" set LLAMA_CTX_SIZE=65536
if "%LLAMA_GPU_LAYERS%"=="" set LLAMA_GPU_LAYERS=999
if "%LLAMA_BATCH_SIZE%"=="" set LLAMA_BATCH_SIZE=512
REM Optional (empty = flag not passed):
REM   LLAMA_CPU_MOE     MoE only: number of layers whose experts stay in system RAM (--n-cpu-moe)
REM   LLAMA_UBATCH_SIZE physical batch size (--ubatch-size)
REM   LLAMA_THREADS     CPU threads, use physical core count (--threads)
REM   LLAMA_FLASH_ATTN  on/off/auto (-fa)
REM   LLAMA_CACHE_TYPE  KV cache type for K and V, e.g. q8_0 (--cache-type-k/-v)
if "%LLAMA_ARGS%"=="" set LLAMA_ARGS=

REM --- required ---
if "%LLAMA_SERVER_EXE%"=="" (
    echo ERROR: LLAMA_SERVER_EXE is not set.
    echo Copy .env.example to .env and fill in your llama-server path.
    exit /b 1
)
if "%LLAMA_MODEL%"=="" (
    echo ERROR: LLAMA_MODEL is not set.
    echo Copy .env.example to .env and fill in your model path.
    exit /b 1
)

REM --- preflight ---
if not exist "%LLAMA_SERVER_EXE%" (
    echo ERROR: llama-server not found: %LLAMA_SERVER_EXE%
    exit /b 1
)
if not exist "%LLAMA_MODEL%" (
    echo ERROR: model not found: %LLAMA_MODEL%
    exit /b 1
)
curl.exe --silent --max-time 2 http://%LLAMA_HOST%:%LLAMA_PORT%/ >nul 2>&1
if not errorlevel 1 (
    echo llama.cpp server already running on %LLAMA_HOST%:%LLAMA_PORT%.
    exit /b 0
)

REM --- optional args ---
set "LLAMA_OPT="
if not "%LLAMA_CPU_MOE%"=="" set "LLAMA_OPT=%LLAMA_OPT% --n-cpu-moe %LLAMA_CPU_MOE%"
if not "%LLAMA_UBATCH_SIZE%"=="" set "LLAMA_OPT=%LLAMA_OPT% --ubatch-size %LLAMA_UBATCH_SIZE%"
if not "%LLAMA_THREADS%"=="" set "LLAMA_OPT=%LLAMA_OPT% --threads %LLAMA_THREADS%"
if not "%LLAMA_FLASH_ATTN%"=="" set "LLAMA_OPT=%LLAMA_OPT% -fa %LLAMA_FLASH_ATTN%"
if not "%LLAMA_CACHE_TYPE%"=="" set "LLAMA_OPT=%LLAMA_OPT% --cache-type-k %LLAMA_CACHE_TYPE% --cache-type-v %LLAMA_CACHE_TYPE%"

REM --- launch ---
REM %LLAMA_ARGS% comes FIRST so every named setting below wins. llama.cpp
REM takes the last occurrence of a repeated flag, so appending the escape
REM hatch at the end would let it silently override a setting this script
REM defaults, validates and reports on.
echo Starting llama.cpp server on %LLAMA_HOST%:%LLAMA_PORT%...
echo   ctx=%LLAMA_CTX_SIZE% gpu-layers=%LLAMA_GPU_LAYERS% cpu-moe=%LLAMA_CPU_MOE% opts:%LLAMA_OPT% extra:%LLAMA_ARGS%
"%LLAMA_SERVER_EXE%" ^
    %LLAMA_ARGS% ^
    -m "%LLAMA_MODEL%" ^
    --ctx-size %LLAMA_CTX_SIZE% ^
    --gpu-layers %LLAMA_GPU_LAYERS% ^
    --batch-size %LLAMA_BATCH_SIZE% ^
    --host %LLAMA_HOST% ^
    --port %LLAMA_PORT% ^
    --jinja ^
    %LLAMA_OPT%
if errorlevel 1 (
    echo ERROR: llama-server exited with code %errorlevel%.
    exit /b %errorlevel%
)
exit /b 0