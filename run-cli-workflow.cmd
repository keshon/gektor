@echo off
setlocal
if "%~1"=="" (
    echo Usage: run-gektor-workflow.cmd [flags] recipe.json
    echo Relative paths use your current directory. Put flags before the task or recipe.
    exit /b 1
)
call "%~dp0scripts\ensure-build.cmd"
if errorlevel 1 exit /b %errorlevel%
"%~dp0gektor.exe" workflow run %*
exit /b %errorlevel%
