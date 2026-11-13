@echo off
REM Compatibility alias. Relative paths use the caller's directory.
call "%~dp0..\run-cli-agent.cmd" %*
exit /b %errorlevel%
