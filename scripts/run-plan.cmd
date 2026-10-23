@echo off
setlocal
cd /d "%~dp0.."

REM Plan mode: read-only tools, propose a plan, change nothing.
REM
REM   run-plan.cmd "restructure the config package"
REM
REM Flags pass through; keep them before the task text.

if "%~1"=="" (
    echo Usage: run-plan.cmd [flags] "task"
    exit /b 1
)

call scripts\ensure-build.cmd
if errorlevel 1 exit /b %errorlevel%

.\gektor.exe -plan %*
exit /b %errorlevel%
