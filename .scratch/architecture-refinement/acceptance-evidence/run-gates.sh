#!/usr/bin/env bash
cd /c/Users/saber/dev/manga-refinement-48
export GOTOOLCHAIN=go1.26.10 GOCACHE=C:/Users/saber/dev/manga_manager/.gocache GOTMPDIR=C:/Users/saber/dev/manga-refinement-48/.tmp/go GOLANGCI_LINT_CACHE=C:/Users/saber/dev/manga-refinement-48/.tmp/lint
run() { local name=$1; shift; echo "$*" > .tmp/evidence/$name.command; "$@" > .tmp/evidence/$name.log 2>&1; echo $? > .tmp/evidence/$name.exit; }
run go-all go test -count=1 -timeout=30m ./...
run race-all go test -race -count=1 -timeout=30m ./...
run vet go vet ./...
run format gofmt -l ./cmd ./internal
run lint golangci-lint run --timeout=8m
run build go build -o .tmp/server.exe ./cmd/server
run tsgen go run ./cmd/tsgen
run ts-drift git diff --exit-code -- web/src/api/generated.ts
run doc-style timeout 180 bash scripts/check-doc-style.sh
