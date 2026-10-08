@echo off
setlocal
cd /d "%~dp0"
if not exist dist mkdir dist

rem Prefer the project-local Go 1.20.14 toolchain, then PATH.
set "GO=go"
if exist "%~dp0.tools\go\bin\go.exe" set "GO=%~dp0.tools\go\bin\go.exe"
"%GO%" version | findstr /C:"go1.20.14 " >nul
if errorlevel 1 (
    echo Go 1.20.14 is required for Windows 7 compatibility.
    exit /b 1
)
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0

echo [1/4] Running tests
"%GO%" test ./...
if errorlevel 1 goto :fail

echo [2/4] Building GUI release: dist\AutoNet.exe
"%GO%" build -trimpath -ldflags="-s -w -H windowsgui" -o dist\AutoNet.exe .
if errorlevel 1 goto :fail

echo [3/4] Building console debug: dist\AutoNet-debug.exe
rem Debug build must NOT use -H windowsgui so CMD waits and stdin works.
"%GO%" build -trimpath -ldflags="-s -w" -o dist\AutoNet-debug.exe .
if errorlevel 1 goto :fail

echo [4/4] Building Walk smoke test
"%GO%" build -trimpath -ldflags="-s -w -H windowsgui" -o dist\walk-smoke.exe .\cmd\walk-smoke
if errorlevel 1 goto :fail
if not exist dist\devices.csv (
    copy /y examples\devices.csv dist\devices.csv >nul
    if errorlevel 1 goto :fail
)
if not exist dist\links.csv (
    copy /y examples\links.csv dist\links.csv >nul
    if errorlevel 1 goto :fail
)
copy /y README.txt dist\README.txt >nul
if errorlevel 1 goto :fail

echo Build OK.
exit /b 0

:fail
echo Build FAILED.
exit /b 1
