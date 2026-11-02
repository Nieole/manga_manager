cd /c/Users/saber/dev/manga-refinement-48
export GOTOOLCHAIN=go1.26.10 GOCACHE=C:/Users/saber/dev/manga_manager/.gocache GOTMPDIR=C:/Users/saber/dev/manga-refinement-48/.tmp/go
run() { local name=$1; shift; echo "$*" > .tmp/evidence/$name.command; "$@" > .tmp/evidence/$name.log 2>&1; echo $? > .tmp/evidence/$name.exit; }
run environment bash -c 'go version; go env GOOS GOARCH CGO_ENABLED CC; ../manga-refinement-48/.tmp/node.exe --version; golangci-lint version; sqlc version; govulncheck -version'
run sqlc powershell.exe -NoProfile -Command 'sqlc generate; exit $LASTEXITCODE'
run sql-drift git diff --exit-code -- internal/database
run vuln timeout 240 govulncheck ./...
run linux-build env CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o .tmp/server-linux ./cmd/server
run darwin-build env CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -o .tmp/server-darwin ./cmd/server
