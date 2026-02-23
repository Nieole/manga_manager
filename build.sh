#!/bin/bash
set -e

echo "Building Frontend..."
cd web
npm install
npm run build
cd ..

echo "Building Manga Manager Backend..."
# Apple Silicon
GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o build/manga-manager-mac-arm64 ./cmd/server
# Linux AMD64
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o build/manga-manager-linux-amd64 ./cmd/server
# Windows
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o build/manga-manager-win-amd64.exe ./cmd/server

echo "Build Completed: ./build"
ls -lh build/
