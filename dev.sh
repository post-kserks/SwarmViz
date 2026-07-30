#!/usr/bin/env bash

# SwarmViz Dev Runner — Запуск бэкенда и Vite dev-сервера одновременно

set -e

# Очистка фоновых процессов при выходе (Ctrl+C или закрытие)
cleanup() {
    echo ""
    echo "🛑 Остановка SwarmViz dev-окружения..."
    if [ -n "$GO_PID" ]; then
        kill "$GO_PID" 2>/dev/null || true
    fi
    if [ -n "$VITE_PID" ]; then
        kill "$VITE_PID" 2>/dev/null || true
    fi
    wait 2>/dev/null || true
    echo "✅ Все процессы остановлены."
    exit 0
}

trap cleanup SIGINT SIGTERM EXIT

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "🚀 Запуск SwarmViz Backend (Go)..."
cd "$ROOT_DIR"
go run main.go --port 8942 --path ./ &
GO_PID=$!

echo "⚡ Запуск SwarmViz Frontend (Vite Dev Server)..."
cd "$ROOT_DIR/frontend"
npm run dev -- --port 3000 &
VITE_PID=$!

echo ""
echo "=================================================="
echo "  SwarmViz Dev-окружение успешно запущено!"
echo "  - Backend API & WebSocket: http://127.0.0.1:8942"
echo "  - Frontend Dev UI:         http://localhost:3000"
echo "=================================================="
echo "Для остановки нажмите Ctrl + C"
echo ""

# Ожидание завершения любых процессов
wait
