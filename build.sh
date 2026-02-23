#!/bin/bash
set -e

echo "Building Frontend..."
cd web
npm install
npm run build
cd ..

echo "Building Manga Manager Backend..."

echo "Building Mac ARM64..."
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o build/manga-manager-mac-arm64 ./cmd/server

echo "Building Linux AMD64..."
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 CC="zig cc -target x86_64-linux-musl" CXX="zig c++ -target x86_64-linux-musl" go build -ldflags="-extldflags=-static -s -w" -o build/manga-manager-linux-amd64 ./cmd/server

echo "Building Windows AMD64..."
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC="zig cc -target x86_64-windows-gnu" CXX="zig c++ -target x86_64-windows-gnu" go build -ldflags="-s -w" -o build/manga-manager-win-amd64.exe ./cmd/server

echo "Build Completed: ./build"
ls -lh build/
