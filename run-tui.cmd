@echo off
setlocal
cd /d "%~dp0"

REM Go resolves saved settings, first-run defaults and explicit overrides.

call scripts\ensure-build.cmd
if errorlevel 1 exit /b %errorlevel%

.\gektor.exe -tui -browser chrome %*
exit /b %errorlevel%
