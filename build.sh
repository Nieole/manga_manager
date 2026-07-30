#!/bin/bash
set -e

echo "Building Frontend..."
cd web
# npm ci 而非 npm install：install 会按 package.json 的语义化范围重新解析依赖并就地改写
# package-lock.json，同一个 commit 在不同时间可能装出不同的依赖树——本地构建的产物因此
# 无法与 CI（用的就是 npm ci）对齐。
npm ci
npm run build
cd ..

echo "Building Manga Manager Backend..."

# 版本信息注入：不注入的话本地构建出来的二进制在「关于」页与日志里恒为 dev/unknown，
# 与 release 工作流的产物不可区分，用户报问题时无法判断跑的是哪个版本。
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
COMMIT="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
BUILD_TIME="$(date -u +'%Y-%m-%dT%H:%M:%SZ')"
VERSION_FLAGS="-X 'main.Version=${VERSION}' -X 'main.Commit=${COMMIT}' -X 'main.BuildTime=${BUILD_TIME}'"
echo "Version: ${VERSION} (${COMMIT})"

echo "Building Mac ARM64..."
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w ${VERSION_FLAGS}" -o build/manga-manager-mac-arm64 ./cmd/server

echo "Building Linux AMD64..."
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 CC="zig cc -target x86_64-linux-musl" CXX="zig c++ -target x86_64-linux-musl" go build -ldflags="-extldflags=-static -s -w ${VERSION_FLAGS}" -o build/manga-manager-linux-amd64 ./cmd/server

echo "Building Windows AMD64..."
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC="zig cc -target x86_64-windows-gnu" CXX="zig c++ -target x86_64-windows-gnu" go build -ldflags="-s -w ${VERSION_FLAGS}" -o build/manga-manager-win-amd64.exe ./cmd/server

echo "Build Completed: ./build"
ls -lh build/
