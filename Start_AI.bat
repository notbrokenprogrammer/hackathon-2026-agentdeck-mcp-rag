@echo off
setlocal
cd /d "%~dp0"
set "OLLAMA_ENABLED=true"
if not defined OLLAMA_URL set "OLLAMA_URL=http://127.0.0.1:11434"
if not defined OLLAMA_MODEL set "OLLAMA_MODEL=qwen3:4b-instruct-2507-q4_K_M"
echo RAG + local model: %OLLAMA_MODEL%
echo Ollama must be running with this model already downloaded.
echo Setup and test questions: docs\crew-rag.md
call Start.bat %*
endlocal & exit /b %ERRORLEVEL%
