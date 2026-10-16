@echo off
setlocal
cd /d "%~dp0"

REM Mission mode: an upfront model-generated plan you approve, then one
REM fresh-context worker per subtask, each verified mechanically.
REM
REM   run-mission.cmd "add input validation to the upload handler"
REM
REM Flags pass through; keep them before the task text.

if "%~1"=="" (
    echo Usage: run-mission.cmd [flags] "task"
    exit /b 1
)

go build -o agent.exe .\cmd\agent
if errorlevel 1 (
    echo ERROR: build failed.
    exit /b 1
)

.\agent.exe -mission %*
exit /b %errorlevel%
