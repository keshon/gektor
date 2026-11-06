@echo off
setlocal
call "%~dp0scripts\ensure-build.cmd"
if errorlevel 1 goto :failed
"%~dp0gektor.exe" -tui -tui-screen workflows %*
if errorlevel 1 goto :failed
exit /b 0

:failed
set "GEKTOR_EXIT=%errorlevel%"
echo.
echo GEKTOR exited with an error. Read the diagnostic above.
pause
exit /b %GEKTOR_EXIT%
