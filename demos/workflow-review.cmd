@echo off
setlocal
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0demo.ps1" -Demo workflow-review
set "GEKTOR_DEMO_EXIT=%errorlevel%"
if /i not "%~1"=="--no-pause" pause
exit /b %GEKTOR_DEMO_EXIT%
