@echo off
if "%~1"=="" (
    echo Usage: run-plan.cmd [flags] "task"
    exit /b 1
)
call "%~dp0..\run-gektor-agent.cmd" -plan %*
exit /b %errorlevel%
