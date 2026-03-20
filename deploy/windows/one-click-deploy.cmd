@echo off
setlocal
PowerShell -NoProfile -ExecutionPolicy Bypass -File "%~dp0deploy.ps1" %*
set "EXIT_CODE=%errorlevel%"
echo.
if "%EXIT_CODE%"=="0" (
    echo [AEGIS] one-click deploy finished successfully.
) else (
    echo [AEGIS] one-click deploy failed, exit code: %EXIT_CODE%
)
echo [AEGIS] press any key to close this window...
pause >nul
exit /b %EXIT_CODE%
