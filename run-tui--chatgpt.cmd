@echo off
setlocal
cd /d "%~dp0"

REM Go resolves saved settings, first-run defaults and explicit overrides.

call scripts\ensure-build.cmd
if errorlevel 1 goto :failed

.\gektor.exe -tui -browser chrome -browser-profile work -backend-kind chatgpt -model gpt-6.1-sol -context-limit 262144 %*
if errorlevel 1 goto :failed
exit /b 0

:failed
set "GEKTOR_EXIT=%errorlevel%"
echo.
echo GEKTOR exited with an error. Read the diagnostic above.
pause
exit /b %GEKTOR_EXIT%