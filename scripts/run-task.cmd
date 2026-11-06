@echo off
REM Compatibility alias. Relative paths use the caller's directory.
call "%~dp0..\run-gektor-agent.cmd" %*
exit /b %errorlevel%
