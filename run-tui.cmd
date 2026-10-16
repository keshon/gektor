@echo off
setlocal
cd /d "%~dp0"

REM TUI launcher (default backend: llama.cpp on 127.0.0.1:8080).
set BACKEND=http://127.0.0.1:8080

curl.exe --silent --max-time 2 %BACKEND%/ >nul 2>&1
if errorlevel 1 (
    echo ERROR: no model server at %BACKEND%.
    echo Start run-server-llama.cmd in another terminal and wait for the model to finish loading.
    exit /b 1
)

go build -o agent.exe .\cmd\agent
if errorlevel 1 (
    echo ERROR: build failed.
    exit /b 1
)

.\agent.exe -tui -backend-kind llama -backend %BACKEND% -workspace . -yes -no-verify %*
exit /b %errorlevel%
