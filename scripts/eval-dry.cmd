@echo off
setlocal
cd /d "%~dp0.."

REM Validate every eval probe's shape and pass criteria. No model calls.
REM Run it after editing anything under eval\probes\ or eval\prompts\.
go run ./cmd/eval -dry %*
exit /b %errorlevel%
