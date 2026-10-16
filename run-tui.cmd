@echo off
setlocal
cd /d "%~dp0"

REM Local TUI launcher for the KoboldCPP backend on 127.0.0.1:5001.
REM Checks the server, builds agent.exe, then opens the fullscreen UI.
REM Needs a real terminal (TTY). Keep flags before any task text.

set BACKEND=http://127.0.0.1:5001

curl.exe --silent --fail --max-time 2 %BACKEND%/api/v1/model >nul 2>&1
if errorlevel 1 (
    echo ERROR: no model server at %BACKEND%.
    echo Start run-kobold.cmd in another terminal and wait for the model
    echo to finish loading, then run this launcher again.
    exit /b 1
)

go build -o agent.exe .\cmd\agent
if errorlevel 1 (
    echo ERROR: build failed.
    exit /b 1
)

.\agent.exe -tui -backend-kind kobold -backend %BACKEND% -workspace . -yes -no-verify %*
exit /b %errorlevel%
