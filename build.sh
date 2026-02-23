#!/bin/bash
set -e

echo "Building Frontend..."
cd web
npm install
npm run build
cd ..

echo "Building Manga Manager Backend (Native with CGO)..."
CGO_ENABLED=1 go build -ldflags="-s -w" -o build/manga-manager ./cmd/server

echo "Build Completed: ./build"
ls -lh build/
