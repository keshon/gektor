@echo off
rem ---------------------------------------------------------------------------
rem  TARS build helper.
rem
rem    build            build every package, then agent.exe
rem    build test       run the unit tests (extra args pass through)
rem    build check      full local CI: vet, build, race tests, conventions,
rem                      eval probes -dry
rem    build fmt        list files gofmt would change
rem    build fmt fix    rewrite files with gofmt -w
rem    build vuln       govulncheck on all packages (installs if missing)
rem    build clean      remove build outputs, logs and eval results
rem    build help       show this list
rem
rem  gofmt runs only when you ask for it. The CI format job runs on a Linux
rem  checkout where files are LF as stored, but a Windows working tree is
rem  CRLF and gofmt -l lists every file there. That listing is a checkout
rem  artifact, not a formatting problem - CI is the authority on formatting.
rem
rem  build test and build check redirect the user's home to a throwaway
rem  directory for the duration of the run, so no test can write into the
rem  real .tars\. Nothing to clean up afterwards, because nothing real was
rem  reachable.
rem
rem  build clean never touches .tars\: that directory holds resumable task
rem  state, and losing it means losing saved runs. It is the only user data
rem  the project keeps, and it lives in the user's home, not in the repo -
rem  so a recursive delete anywhere near it is never the answer to anything.
rem
rem  Messages are English on purpose: a .cmd file with non-ASCII text is at
rem  the mercy of whatever code page the console happens to be in, and a
rem  build script that garbles its own output is worse than one that speaks
rem  English.
rem
rem  Save this file as plain ASCII, never "UTF-8 with BOM". cmd reads the
rem  three BOM bytes as part of the first line, "@echo off" fails as an
rem  unknown command, and every line after it is echoed to the console.
rem ---------------------------------------------------------------------------
setlocal
cd /d "%~dp0"

set MODE=%~1
if "%MODE%"=="" set MODE=build

where go >nul 2>nul
if errorlevel 1 (
    echo ERROR: Go was not found. Install Go 1.26.6 or newer first.
    exit /b 1
)

if /i "%MODE%"=="help" goto :help
if /i "%MODE%"=="build" goto :build
if /i "%MODE%"=="test" goto :test
if /i "%MODE%"=="check" goto :check
if /i "%MODE%"=="fmt" goto :fmt
if /i "%MODE%"=="vuln" goto :vuln
if /i "%MODE%"=="clean" goto :clean

echo ERROR: unknown mode "%MODE%".
echo.
call :help
exit /b 1

rem ---------------------------------------------------------------------------
rem  Build every package first (catches compile errors anywhere), then link
rem  the agent binary the run-*.cmd launchers expect at the repo root.
rem ---------------------------------------------------------------------------
:build
echo Building packages...
go build ./...
if errorlevel 1 (
    echo ERROR: build failed.
    exit /b 1
)

echo Building agent.exe...
go build -o agent.exe .\cmd\agent
if errorlevel 1 (
    echo ERROR: agent.exe build failed.
    exit /b 1
)

echo Done: agent.exe
exit /b 0

rem ---------------------------------------------------------------------------
rem  Unit tests. Extra arguments pass through, so these work:
rem    build test -run TUI
rem    build test -race -count 1
rem ---------------------------------------------------------------------------
:test
echo Running tests...
call :sandbox
set EXTRAS=
shift

:collect
if "%~1"=="" goto :run_test
set EXTRAS=%EXTRAS% "%~1"
shift
goto :collect

:run_test
go test ./... %EXTRAS%
exit /b %errorlevel%

rem ---------------------------------------------------------------------------
rem  Point the user's home at a throwaway directory for the duration of a run.
rem
rem  Packages that persist anything call internal/tars, which resolves the
rem  user's real .tars. Four of them redirect it in TestMain, but a package
rem  that forgets, or a test that shells out to a binary that does not, would
rem  write into the live session library. Redirecting USERPROFILE makes the
rem  real one unreachable, so there is nothing left to clean up afterwards.
rem
rem  os.UserHomeDir reads USERPROFILE on Windows and HOME elsewhere; HOME is
rem  set too so a subshell, an MCP helper or a scripted tool lands in the
rem  sandbox as well. setlocal scopes both, so the caller's home is intact
rem  when this script returns and no caller has to undo anything.
rem ---------------------------------------------------------------------------
:sandbox
if not defined TARS_TEST_SANDBOX (
    set "TARS_TEST_SANDBOX=%TEMP%\tars-test-home-%RANDOM%%RANDOM%"
    mkdir "%TARS_TEST_SANDBOX%" 2>nul
)
set "USERPROFILE=%TARS_TEST_SANDBOX%"
set "HOME=%TARS_TEST_SANDBOX%"
exit /b 0

rem ---------------------------------------------------------------------------
rem  Everything CI runs before merging, in the same order, minus gofmt
rem  (see the header). Stop at the first failure; the step name above it
rem  says what broke.
rem ---------------------------------------------------------------------------
:check
echo == vet
go vet ./...
if errorlevel 1 exit /b 1

echo == build
go build ./...
if errorlevel 1 exit /b 1

echo == test -race
call :sandbox
go test -race ./...
if errorlevel 1 exit /b 1

echo == conventions
go test ./internal/conventions/ -run TestConventions
if errorlevel 1 exit /b 1

echo == eval probes -dry
go run ./cmd/eval -dry
if errorlevel 1 exit /b 1

echo.
echo All checks passed.
exit /b 0

rem ---------------------------------------------------------------------------
rem  Formatting. Advisory locally, enforced by CI on a Linux checkout.
rem ---------------------------------------------------------------------------
:fmt
if /i "%~2"=="fix" goto :fmt_fix

echo Checking formatting (gofmt -l)...
echo Reminder: on a CRLF Windows working tree this lists every file.
echo CI runs on a Linux LF checkout and is the formatting authority.
gofmt -l .
exit /b 0

:fmt_fix
echo Rewriting files with gofmt -w ...
echo Note: this converts line endings to LF. With core.autocrlf=true git
echo restores CRLF on the next checkout.
gofmt -w .
exit /b %errorlevel%

rem ---------------------------------------------------------------------------
rem  Vulnerability scan. Mirrors the CI vuln job. The tool is installed
rem  into GOPATH\bin; add it to PATH if the command is not found after
rem  the install step succeeds.
rem ---------------------------------------------------------------------------
:vuln
where govulncheck >nul 2>nul
if errorlevel 1 (
    echo Installing govulncheck...
    go install golang.org/x/vuln/cmd/govulncheck@latest
    if errorlevel 1 (
        echo ERROR: could not install govulncheck.
        exit /b 1
    )
)

echo Running govulncheck...
govulncheck ./...
exit /b %errorlevel%

rem ---------------------------------------------------------------------------
rem  Remove what building and evaluating leave behind. Resumable task state
rem  in .tars\ and local launcher files are never touched.
rem ---------------------------------------------------------------------------
:clean
echo Removing build outputs, logs and eval results...

if exist agent.exe del /q agent.exe
if exist eval.exe del /q eval.exe
if exist agent-debug.log del /q agent-debug.log
if exist tmp rmdir /s /q tmp
if exist temp rmdir /s /q temp
if exist eval\results rmdir /s /q eval\results
if exist eval\log rmdir /s /q eval\log

echo Done. Saved task state in .tars\ was not touched.
exit /b 0

rem ---------------------------------------------------------------------------
:help
echo TARS build helper. Usage: build [mode] [args]
echo.
echo   build            build every package, then agent.exe
echo   build test       run the unit tests (extra args pass through)
echo   build check      full local CI: vet, build, race tests, conventions,
echo                    eval probes -dry
echo   build fmt        list files gofmt would change
echo   build fmt fix    rewrite files with gofmt -w
echo   build vuln       govulncheck on all packages (installs if missing)
echo   build clean      remove build outputs, logs and eval results
echo   build help       show this list
exit /b 0
