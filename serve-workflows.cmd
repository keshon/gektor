@echo off
setlocal
call "%~dp0scripts\ensure-build.cmd"
if errorlevel 1 exit /b %errorlevel%
"%~dp0gektor.exe" workflow serve %*
exit /b %errorlevel%
