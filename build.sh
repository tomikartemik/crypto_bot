#!/bin/bash

# Скрипт сборки для supertrend_bot
# Создает исполняемые файлы для macOS и Windows

set -e

echo "🚀 Начинаем сборку supertrend_bot..."

# Создаем папку dist если её нет
mkdir -p dist

# Очищаем предыдущие сборки
echo "🧹 Очищаем предыдущие сборки..."
rm -rf dist/*

# Сборка для macOS (текущая платформа)
echo "🍎 Собираем для macOS..."

# Консольная версия для macOS
echo "  - Консольная версия (macOS)..."
GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o dist/supertrend_bot_macos_amd64 ./cmd/main.go
GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o dist/supertrend_bot_macos_arm64 ./cmd/main.go

# GUI версия для macOS (только для текущей архитектуры)
echo "  - GUI версия (macOS)..."
echo "    ⚠️  GUI версия собирается только для текущей архитектуры из-за ограничений Fyne"
go build -ldflags="-s -w" -o dist/supertrend_bot_gui_macos ./cmd/gui.go

# Сборка для Windows
echo "🪟 Собираем для Windows..."

# Консольная версия для Windows
echo "  - Консольная версия (Windows)..."
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o dist/supertrend_bot_windows_amd64.exe ./cmd/main.go
GOOS=windows GOARCH=386 go build -ldflags="-s -w" -o dist/supertrend_bot_windows_386.exe ./cmd/main.go

# GUI версия для Windows (пропускаем из-за ограничений Fyne)
echo "  - GUI версия (Windows)..."
echo "    ⚠️  GUI версия для Windows пропущена из-за ограничений Fyne при кроссплатформенной сборке"

# Сборка для Linux (бонус)
echo "🐧 Собираем для Linux..."

# Консольная версия для Linux
echo "  - Консольная версия (Linux)..."
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o dist/supertrend_bot_linux_amd64 ./cmd/main.go
GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o dist/supertrend_bot_linux_arm64 ./cmd/main.go

# GUI версия для Linux (пропускаем из-за ограничений Fyne)
echo "  - GUI версия (Linux)..."
echo "    ⚠️  GUI версия для Linux пропущена из-за ограничений Fyne при кроссплатформенной сборке"

echo "✅ Сборка завершена!"
echo ""
echo "📁 Созданные файлы:"
ls -la dist/

echo ""
echo "📋 Инструкции по запуску:"
echo "  Консольные версии:"
echo "  macOS (Intel):     ./dist/supertrend_bot_macos_amd64"
echo "  macOS (Apple Silicon): ./dist/supertrend_bot_macos_arm64"
echo "  Windows (64-bit):  dist\\supertrend_bot_windows_amd64.exe"
echo "  Windows (32-bit):  dist\\supertrend_bot_windows_386.exe"
echo "  Linux (64-bit):    ./dist/supertrend_bot_linux_amd64"
echo "  Linux (ARM64):     ./dist/supertrend_bot_linux_arm64"
echo ""
echo "  GUI версии:"
echo "  macOS (текущая архитектура): ./dist/supertrend_bot_gui_macos"
echo ""
echo "ℹ️  Примечание: GUI версии для Windows и Linux нужно собирать на соответствующих платформах"
echo "   или использовать Docker для кроссплатформенной сборки GUI приложений."
