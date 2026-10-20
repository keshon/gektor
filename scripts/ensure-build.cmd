@echo off
REM Called from launchers after changing to the application directory.
REM Source checkouts rebuild; binary distributions need no Go installation.
if exist go.mod goto :build
if exist gektor.exe exit /b 0
echo ERROR: gektor.exe is missing. Extract the complete release archive.
exit /b 1

:build
go build -o gektor.exe .\cmd\gektor
exit /b %errorlevel%
