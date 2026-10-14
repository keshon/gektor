@echo off
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\import-voice-assets.ps1" %*
exit /b %errorlevel%
