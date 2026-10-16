@echo off
setlocal
cd /d "%~dp0"

REM List saved tasks, or resume one.
REM
REM   run-resume.cmd              list .tars\tasks (newest first)
REM   run-resume.cmd 20261016-... resume that task id
REM   run-resume.cmd path\to\dir resume a task directory or state file
REM
REM Extra flags pass through when resuming (e.g. -answer "text" for a run
REM that paused on ask_user). The id form resolves under .tars\tasks\.

if "%~1"=="" goto :list

set RESUME=%~1
if exist "%RESUME%" goto :resume
set RESUME=.tars\tasks\%~1
if exist "%RESUME%" goto :resume

echo ERROR: no task directory or state file at "%~1"
echo and no task id "%~1" under .tars\tasks\.
echo Run run-resume.cmd without arguments to list saved tasks.
exit /b 1

:resume
go build -o agent.exe .\cmd\agent
if errorlevel 1 (
    echo ERROR: build failed.
    exit /b 1
)

.\agent.exe -resume "%RESUME%" %2 %3 %4 %5 %6 %7 %8 %9
exit /b %errorlevel%

:list
if not exist ".tars\tasks" (
    echo No saved tasks: .tars\tasks does not exist in this workspace.
    echo Run a task first with run-task.cmd or the TUI.
    exit /b 0
)

echo Saved tasks in .tars\tasks (newest first):
dir /b /o:-d ".tars\tasks"
exit /b 0
