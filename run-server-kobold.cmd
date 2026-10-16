@echo off
setlocal
cd /d "%~dp0"

REM KoboldCPP server launcher (secondary backend).
REM
REM   run-server-kobold.cmd
REM   set KOBOLD_PORT=5002 && run-server-kobold.cmd

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
if "%KOBOLD_PORT%"=="" set KOBOLD_PORT=5001
if "%KOBOLD_HOST%"=="" set KOBOLD_HOST=127.0.0.1
if "%KOBOLD_USE_CUDA%"=="" set KOBOLD_USE_CUDA=0
if "%KOBOLD_GPU_LAYERS%"=="" set KOBOLD_GPU_LAYERS=999
if "%KOBOLD_CONTEXT_SIZE%"=="" set KOBOLD_CONTEXT_SIZE=16384
if "%KOBOLD_QUANT_KV%"=="" set KOBOLD_QUANT_KV=1
if "%KOBOLD_ARGS%"=="" set KOBOLD_ARGS=

REM --- required ---
if "%KOBOLD_EXE%"=="" (
    echo ERROR: KOBOLD_EXE is not set.
    echo Copy .env.example to .env and fill in your KoboldCPP executable path.
    exit /b 1
)
if "%KOBOLD_MODEL%"=="" (
    echo ERROR: KOBOLD_MODEL is not set.
    echo Copy .env.example to .env and fill in your model path.
    exit /b 1
)

REM --- preflight ---
if not exist "%KOBOLD_EXE%" (
    echo ERROR: KoboldCPP not found: %KOBOLD_EXE%
    exit /b 1
)
if not exist "%KOBOLD_MODEL%" (
    echo ERROR: model not found: %KOBOLD_MODEL%
    exit /b 1
)
curl.exe --silent --fail --max-time 2 http://%KOBOLD_HOST%:%KOBOLD_PORT%/api/extra/version >nul 2>&1
if not errorlevel 1 (
    echo KoboldCPP is already running on %KOBOLD_HOST%:%KOBOLD_PORT%.
    exit /b 0
)

REM --- launch ---
for %%e in ("%KOBOLD_EXE%") do set "KOBOLD_DIR=%%~dpe"
cd /d "%KOBOLD_DIR%"

echo Starting KoboldCPP on %KOBOLD_HOST%:%KOBOLD_PORT%...
"%KOBOLD_EXE%" --model "%KOBOLD_MODEL%" ^
    --usecuda %KOBOLD_USE_CUDA% ^
    --gpulayers %KOBOLD_GPU_LAYERS% ^
    --contextsize %KOBOLD_CONTEXT_SIZE% ^
    --quantkv %KOBOLD_QUANT_KV% ^
    --host %KOBOLD_HOST% ^
    --port %KOBOLD_PORT% ^
    --skiplauncher ^
    %KOBOLD_ARGS%
if errorlevel 1 (
    echo ERROR: KoboldCPP exited with code %errorlevel%.
    exit /b %errorlevel%
)
exit /b 0