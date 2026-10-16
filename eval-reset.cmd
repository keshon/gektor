@echo off
setlocal
cd /d "%~dp0"

REM Reset eval\fixtures to the committed baseline and remove artifacts
REM from prior manual eval runs. Safe to run repeatedly.

git checkout -- eval/fixtures
if errorlevel 1 (
    echo ERROR: git checkout of eval\fixtures failed.
    exit /b 1
)

REM Probe-created artifacts only. The committed baseline files
REM (dup.txt, hello.txt, sample.go, README.md, big.txt) come back
REM from git above, so nothing here may name them.
del /q eval\fixtures\goodbye.txt 2>nul
del /q eval\fixtures\alpha.txt 2>nul
del /q eval\fixtures\beta.txt 2>nul
del /q eval\fixtures\gamma.txt 2>nul
del /q eval\fixtures\notes.txt 2>nul

echo Fixtures reset to the git baseline (dup.txt, hello.txt, sample.go,
echo README.md, big.txt).
