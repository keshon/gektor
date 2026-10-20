@echo off
setlocal
cd /d "%~dp0"

REM List saved tasks, or resume one.
REM
REM   run-resume.cmd              list .gektor\tasks (newest first)
REM   run-resume.cmd 20261016-... resume that task id
REM   run-resume.cmd path\to\dir resume a task directory or state file
REM
REM Extra flags pass through when resuming (e.g. -answer "text" for a run
REM that paused on ask_user). The id form resolves under "%STATE_ROOT%\tasks".

set "STATE_ROOT=.gektor"
if defined USERPROFILE set "STATE_ROOT=%USERPROFILE%\.gektor"
if not exist "%STATE_ROOT%" if defined USERPROFILE if exist "%USERPROFILE%\.tars\" set "STATE_ROOT=%USERPROFILE%\.tars"

if "%~1"=="" goto :list

set "RESUME=%~1"
if exist "%RESUME%" goto :resume
set "RESUME=%STATE_ROOT%\tasks\%~1"
if exist "%RESUME%" goto :resume

echo ERROR: no task directory or state file at "%~1"
echo and no task id "%~1" under "%STATE_ROOT%\tasks".
echo Run run-resume.cmd without arguments to list saved tasks.
exit /b 1

:resume
go build -o gektor.exe .\cmd\gektor
if errorlevel 1 (
    echo ERROR: build failed.
    exit /b 1
)

.\gektor.exe -resume "%RESUME%" %2 %3 %4 %5 %6 %7 %8 %9
exit /b %errorlevel%

:list
if not exist "%STATE_ROOT%\tasks" (
    echo No saved tasks: "%STATE_ROOT%\tasks" does not exist.
    echo Run a task first with run-task.cmd or the TUI.
    exit /b 0
)

echo Saved tasks in "%STATE_ROOT%\tasks" (newest first):
dir /b /o:-d "%STATE_ROOT%\tasks"
exit /b 0
