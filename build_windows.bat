@echo off
REM Скрипт сборки для Windows
REM Создает исполняемые файлы для Windows

echo 🚀 Начинаем сборку supertrend_bot для Windows...

REM Создаем папку dist если её нет
if not exist dist mkdir dist

REM Очищаем предыдущие сборки
echo 🧹 Очищаем предыдущие сборки...
del /q dist\* 2>nul

REM Сборка для Windows
echo 🪟 Собираем для Windows...

REM Консольная версия для Windows
echo   - Консольная версия (Windows)...
go build -ldflags="-s -w" -o dist\supertrend_bot_windows_amd64.exe .\cmd\main.go
go build -ldflags="-s -w" -o dist\supertrend_bot_windows_386.exe .\cmd\main.go

REM GUI версия для Windows
echo   - GUI версия (Windows)...
go build -ldflags="-s -w" -o dist\supertrend_bot_gui_windows_amd64.exe .\cmd\gui.go
go build -ldflags="-s -w" -o dist\supertrend_bot_gui_windows_386.exe .\cmd\gui.go

echo ✅ Сборка завершена!
echo.
echo 📁 Созданные файлы:
dir dist\

echo.
echo 📋 Инструкции по запуску:
echo   Консольные версии:
echo   Windows (64-bit):  dist\supertrend_bot_windows_amd64.exe
echo   Windows (32-bit):  dist\supertrend_bot_windows_386.exe
echo.
echo   GUI версии:
echo   Windows (64-bit):  dist\supertrend_bot_gui_windows_amd64.exe
echo   Windows (32-bit):  dist\supertrend_bot_gui_windows_386.exe
echo.
echo ℹ️  Убедитесь, что у вас есть файлы конфигурации:
echo    - bot_config.json
echo    - trader_configs.json


