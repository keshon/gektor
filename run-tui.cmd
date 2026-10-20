@echo off
setlocal
cd /d "%~dp0"

REM Go resolves saved settings, first-run defaults and explicit overrides.

go build -o gektor.exe .\cmd\gektor
if errorlevel 1 (
    echo ERROR: build failed.
    exit /b 1
)

.\gektor.exe -tui %*
exit /b %errorlevel%
