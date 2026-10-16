@echo off
setlocal
cd /d "%~dp0"

REM Run the eval suite. The backend must already be running.
REM
REM   eval-run.cmd                       everything, once each
REM   eval-run.cmd -tier smoke           quick subset
REM   eval-run.cmd -only 05 -runs 3      one probe, three runs
REM   eval-run.cmd -require-model qwen   refuse other weights
REM
REM All flags pass through to cmd/eval; see eval\README.md. Results land
REM in eval\results\<timestamp>\ (gitignored).

if "%~1"=="" goto :run

go run ./cmd/eval %*
exit /b %errorlevel%

:run
go run ./cmd/eval
exit /b %errorlevel%
