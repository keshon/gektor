@echo off
setlocal
cd /d "%~dp0.."

REM Run one task in print mode: build gektor.exe, then run it.
REM
REM   run-task.cmd "add a Version constant to config.go"
REM   run-task.cmd -max-steps 40 -debug "fix the failing test"
REM
REM Flags pass through to gektor.exe. Keep them before the task text:
REM Go's flag parser stops at the first positional argument.
REM The backend defaults to llama.cpp at http://127.0.0.1:8080.
REM Start the model server separately, or pass -backend/-backend-kind.

if "%~1"=="" (
    echo Usage: run-task.cmd [flags] "task"
    echo Flags pass through to gektor.exe; keep them before the task text.
    exit /b 1
)

call scripts\ensure-build.cmd
if errorlevel 1 exit /b %errorlevel%

.\gektor.exe %*
exit /b %errorlevel%
