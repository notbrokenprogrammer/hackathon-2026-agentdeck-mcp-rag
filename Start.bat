@echo off
setlocal
cd /d "%~dp0"
if not defined OLLAMA_ENABLED set "OLLAMA_ENABLED=true"
set "DEV_WEB_DIR=%~dp0web"
if not exist "web\css\flight-deck.css" (echo Frontend missing. Download the complete project. & pause & exit /b 1)
echo Open http://localhost:8080 in your browser.
echo Other players: use Invite in the game and join the same room.
if exist "bin\game-server.exe" (
  bin\game-server.exe %*
) else (
  where go >nul 2>nul
  if errorlevel 1 (echo Install Go 1.23+ to run from source. See README.md. & pause & exit /b 1)
  go run . %*
)
set "LASTCREW_EXIT_CODE=%ERRORLEVEL%"
pause
endlocal & exit /b %LASTCREW_EXIT_CODE%
