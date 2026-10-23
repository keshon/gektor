@echo off
setlocal
cd /d "%~dp0.."

REM Evaluate against an already running model server.
REM Defaults: llama.cpp at http://127.0.0.1:8080.
REM Examples:
REM   scripts\eval-run.cmd -suite core -runs 3 -jobs 1
REM   scripts\eval-run.cmd -backend http://127.0.0.1:9931 -only 05
REM All arguments pass through; see eval\README.md.

go run ./cmd/eval %*
exit /b %errorlevel%
