@echo off
setlocal EnableExtensions
cd /d "%~dp0.."

REM =====================================================================
REM  bench-llama.cmd - llama-bench matrix runner + comparison report
REM
REM  Usage:
REM    bench-llama.cmd                      full matrix, settings from .env
REM    bench-llama.cmd quick                reduced matrix (much faster)
REM    bench-llama.cmd path\to\model.gguf   benchmark another model
REM    (arguments can be combined, e.g.: bench-llama.cmd quick model.gguf)
REM
REM  Reads from the repository-root .env:
REM    BENCH_EXE          path to llama-bench.exe
REM    LLAMA_MODEL        model to test (or pass it as an argument)
REM    LLAMA_GPU_LAYERS   default 999
REM    LLAMA_THREADS      optional
REM  Optional BENCH_* overrides (in .env or environment) - see defaults below.
REM
REM  Needs bench-report.ps1 next to this file.
REM  Stop llama-server first: it occupies VRAM and would skew the results.
REM =====================================================================

REM ---- defaults (override with BENCH_* in .env) -----------------------
REM n-cpu-moe values to try (MoE models). Use "-" for dense models.
set "BENCH_NCMOE=26 30 34 38"
REM KV cache types to try (K and V use the same type)
set "BENCH_KV=f16 q8_0"
REM ubatch sizes, comma separated, must be <= BENCH_BATCH
set "BENCH_UBATCH=512,1024"
REM context depths (tokens already in context), comma separated
set "BENCH_DEPTH=0,8192"
set "BENCH_BATCH=2048"
set "BENCH_PP=2048"
set "BENCH_TG=128"
set "BENCH_REPS=2"
set "BENCH_FA=1"
REM report: typical agent step = read N new tokens, then generate M tokens
set "BENCH_STEP_PROMPT=3000"
set "BENCH_STEP_GEN=300"
REM report: safety margin added to n-cpu-moe for the recommendation
set "BENCH_MARGIN=2"

REM ---- load .env (only LLAMA_* and BENCH_* keys with a value) ---------
set "ENV_FILE=.env"
if exist "%ENV_FILE%" call :load_env

REM ---- arguments ------------------------------------------------------
set "QUICK="
:args
if "%~1"=="" goto args_done
if /i "%~1"=="quick" (set "QUICK=1") else set "LLAMA_MODEL=%~1"
shift
goto args

:args_done
if defined QUICK call :apply_quick

REM ---- resolve paths --------------------------------------------------
if not defined LLAMA_GPU_LAYERS set "LLAMA_GPU_LAYERS=999"
if not defined BENCH_EXE goto err_noexe
if not exist "%BENCH_EXE%" goto err_exe
if not defined LLAMA_MODEL goto err_nomodel
if not exist "%LLAMA_MODEL%" goto err_model
if not exist "%~dp0bench-report.ps1" goto err_report

REM ---- sanity checks --------------------------------------------------
tasklist /fi "imagename eq llama-server.exe" 2>nul | find /i "llama-server.exe" >nul
if not errorlevel 1 goto err_running

if "%BENCH_NCMOE%"=="-" goto skip_moe_check
"%BENCH_EXE%" --help 2>&1 | findstr /c:"n-cpu-moe" >nul
if errorlevel 1 echo WARNING: this llama-bench does not list --n-cpu-moe. Update llama.cpp or runs may fail.
:skip_moe_check

REM ---- output folder --------------------------------------------------
for /f %%T in ('powershell -NoProfile -Command "Get-Date -Format yyyyMMdd-HHmmss"') do set "STAMP=%%T"
set "OUT_DIR=%CD%\bench-results\%STAMP%"
mkdir "%OUT_DIR%" >nul 2>&1
> "%OUT_DIR%\runs.csv" echo moe,kv,exit

set /a TOTAL=0
for %%M in (%BENCH_NCMOE%) do for %%K in (%BENCH_KV%) do set /a TOTAL+=1

echo.
echo ============================================================
echo  llama-bench matrix
echo ============================================================
echo  Model    : %LLAMA_MODEL%
echo  ngl      : %LLAMA_GPU_LAYERS%   fa: %BENCH_FA%   batch: %BENCH_BATCH%
echo  n-cpu-moe: %BENCH_NCMOE%
echo  KV cache : %BENCH_KV%
echo  ubatch   : %BENCH_UBATCH%
echo  depth    : %BENCH_DEPTH%   pp: %BENCH_PP%   tg: %BENCH_TG%   reps: %BENCH_REPS%
echo  Runs     : %TOTAL%  (each one loads the model)
echo  Results  : %OUT_DIR%
echo ============================================================
echo.

set /a N=0
for %%M in (%BENCH_NCMOE%) do (
    for %%K in (%BENCH_KV%) do (
        set /a N+=1
        call :run %%M %%K
    )
)

echo.
echo Building report...
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0bench-report.ps1" -Dir "%OUT_DIR%" -StepPrompt %BENCH_STEP_PROMPT% -StepGen %BENCH_STEP_GEN% -Margin %BENCH_MARGIN%
goto done

REM =====================================================================
:run
set "R_MOE=%~1"
set "R_KV=%~2"
set "R_FLAGS="
if not "%R_MOE%"=="-" set "R_FLAGS=--n-cpu-moe %R_MOE%"
if defined LLAMA_THREADS set "R_FLAGS=%R_FLAGS% -t %LLAMA_THREADS%"
set "R_BASE=%OUT_DIR%\ncmoe-%R_MOE%_kv-%R_KV%"
echo [%N%/%TOTAL%] n-cpu-moe=%R_MOE%  kv=%R_KV%
"%BENCH_EXE%" -m "%LLAMA_MODEL%" -ngl %LLAMA_GPU_LAYERS% %R_FLAGS% -fa %BENCH_FA% -ctk %R_KV% -ctv %R_KV% -b %BENCH_BATCH% -ub %BENCH_UBATCH% -p %BENCH_PP% -n %BENCH_TG% -d %BENCH_DEPTH% -r %BENCH_REPS% -o csv > "%R_BASE%.csv" 2> "%R_BASE%.log"
set "R_EXIT=%errorlevel%"
>> "%OUT_DIR%\runs.csv" echo %R_MOE%,%R_KV%,%R_EXIT%
if not "%R_EXIT%"=="0" echo     FAILED (exit %R_EXIT%) - probably out of VRAM, see %R_BASE%.log
goto :eof

:load_env
for /f "usebackq tokens=1,* delims==" %%A in (`findstr /r /c:"^LLAMA_[A-Z_]*=." /c:"^BENCH_[A-Z_]*=." "%ENV_FILE%"`) do set "%%A=%%B"
goto :eof

:apply_quick
set "BENCH_KV=q8_0"
set "BENCH_UBATCH=512"
set "BENCH_DEPTH=0"
set "BENCH_REPS=1"
goto :eof

REM ---- errors ---------------------------------------------------------
:err_noexe
echo ERROR: BENCH_EXE is not set. Put the path to llama-bench.exe in .env.
exit /b 1
:err_exe
echo ERROR: llama-bench.exe not found: %BENCH_EXE%
echo        It ships in the same llama.cpp release archive as llama-server.exe.
exit /b 1
:err_nomodel
echo ERROR: LLAMA_MODEL is not set. Put it in .env or pass the .gguf path as an argument.
exit /b 1
:err_model
echo ERROR: model file not found: %LLAMA_MODEL%
exit /b 1
:err_report
echo ERROR: bench-report.ps1 must be in the same folder as this script.
exit /b 1
:err_running
echo ERROR: llama-server.exe is running. Stop it first - it occupies VRAM and skews the benchmark.
exit /b 1

:done
REM pause only when started by double-click
echo %cmdcmdline% | find /i "/c" >nul && pause
exit /b 0
