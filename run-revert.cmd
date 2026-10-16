@echo off
setlocal
cd /d "%~dp0"

REM Workspace checkpoint revert.
REM
REM   run-revert.cmd            preview what a revert would restore
REM   run-revert.cmd apply      restore the latest checkpoint (asks first)
REM   run-revert.cmd apply -revert-from PATH
REM                             restore a specific older archive
REM
REM TARS checkpoints tracked and nonignored files and the Git index before
REM a run in a Git workspace. Preview lists the actions; apply performs
REM them. Ignored files, .tars\ state and Git history are excluded, and
REM restore refuses a changed HEAD.

if /i "%~1"=="apply" goto :apply
goto :preview

:preview
go build -o agent.exe .\cmd\agent
if errorlevel 1 (
    echo ERROR: build failed.
    exit /b 1
)

.\agent.exe -revert-preview %2 %3 %4 %5 %6 %7 %8 %9
exit /b %errorlevel%

:apply
echo This restores the workspace to the pre-run checkpoint.
echo Files created by the run are removed; pre-existing dirty and
echo staged work comes back. Ignored files are not affected.

choice /c YN /m "Continue"
if errorlevel 2 exit /b 0

go build -o agent.exe .\cmd\agent
if errorlevel 1 (
    echo ERROR: build failed.
    exit /b 1
)

.\agent.exe -revert %2 %3 %4 %5 %6 %7 %8 %9
exit /b %errorlevel%
