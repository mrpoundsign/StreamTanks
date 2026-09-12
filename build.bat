@echo off
REM StreamTanks Build, Test & Lint Runner
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0build.ps1" %*
if %ERRORLEVEL% NEQ 0 (
    exit /b %ERRORLEVEL%
)
