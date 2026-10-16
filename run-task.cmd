@echo off
setlocal
cd /d "%~dp0"

REM Run one task in print mode: build agent.exe, then run it.
REM
REM   run-task.cmd "add a Version constant to config.go"
REM   run-task.cmd -max-steps 40 -debug "fix the failing test"
REM
REM Flags pass through to agent.exe. Keep them before the task text:
REM Go's flag parser stops at the first positional argument.
REM The backend defaults to http://localhost:5001 - start run-kobold.cmd
REM first, or pass -backend/-backend-kind for something else.

if "%~1"=="" (
    echo Usage: run-task.cmd [flags] "task"
    echo Flags pass through to agent.exe; keep them before the task text.
    exit /b 1
)

go build -o agent.exe .\cmd\agent
if errorlevel 1 (
    echo ERROR: build failed.
    exit /b 1
)

.\agent.exe %*
exit /b %errorlevel%
