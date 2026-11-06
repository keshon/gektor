@echo off
setlocal
REM Build from the application folder without changing the caller's directory.
pushd "%~dp0.." || exit /b 1
if exist go.mod goto :build
if exist gektor.exe goto :success
echo ERROR: gektor.exe is missing. Extract the complete release archive.
set "GEKTOR_BUILD_EXIT=1"
goto :done

:build
go build -o gektor.exe .\cmd\gektor
set "GEKTOR_BUILD_EXIT=%errorlevel%"
goto :done

:success
set "GEKTOR_BUILD_EXIT=0"

:done
popd
exit /b %GEKTOR_BUILD_EXIT%
