@echo off
REM Start imgconv's graphical interface.
REM
REM It REBUILDS first, every time. The interface is compiled into the binary with
REM go:embed, so editing the page changes nothing until the binary is rebuilt, and
REM running a stale binary looks exactly like a change that did not work.
REM
REM Closing this window stops the server. Nothing is left running.
setlocal
cd /d "%~dp0"

where go >nul 2>nul
if errorlevel 1 (
  echo imgconv: Go is not installed.
  echo Either install Go, or download a ready-made binary from
  echo   https://github.com/mateusands/imgconv/releases
  pause
  exit /b 1
)

go build -o imgconv.exe .\cmd\imgconv
if errorlevel 1 (
  echo imgconv: the build failed.
  pause
  exit /b 1
)

imgconv.exe --ui %*
