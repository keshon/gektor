@echo off
setlocal
cd /d "%~dp0"

REM Go resolves saved settings, first-run defaults and explicit overrides.

go build -o agent.exe .\cmd\agent
if errorlevel 1 (
    echo ERROR: build failed.
    exit /b 1
)

.\agent.exe -tui %*
exit /b %errorlevel%
