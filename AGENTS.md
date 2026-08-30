# Repository Guidelines

## Project Structure & Module Organization
`cmd/server/main.go` is the backend entrypoint. Core Go packages live under `internal/`:
`api/` for HTTP handlers, `scanner/` for library discovery, `metadata/` for scraping and LLM prompts, `database/` for SQLite and generated `sqlc` code, and `parser/`, `images/`, `koreader/`, `config/`, `runtimecfg/`, `logger/`, `booksort/`, `proposal/`, `external/`, `storageio/`, `diskwork/`, `taskcontrol/`, `taskrun/` for supporting services (full-text search is handled by SQLite FTS5 inside `database/`, not a separate package). SQL sources are in `sql/query.sql` and `internal/database/schema.sql`. The Vite frontend lives in `web/`; routes are in `web/src/App.tsx`, pages in `web/src/pages/`, shared UI in `web/src/components/`, and locale/theme helpers in `web/src/i18n/` and `web/src/theme/`. Screenshots and marketing assets are under `images/`.

## Build, Test, and Development Commands
Run the frontend locally with `cd web && npm run dev`. Build the frontend with `cd web && npm run build`; this is the standard verification step for UI changes. Lint the frontend with `cd web && npm run lint`. Run backend tests with a repo-local Go cache to avoid sandbox/cache permission issues:
```bash
# Use a repo-local cache directory (relative paths, works on any machine/OS):
GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...
```
Use `./build.sh` for a full release-style build; it installs frontend dependencies, builds `web`, and cross-compiles binaries into `build/`. 升级前端依赖前读 `web/README.md`：两条 `npm audit` high 已确认不可达，`npm audit fix --force` 会把 `react-router-dom` 降级 7 个小版本。

## Coding Style & Naming Conventions
Go code should stay `gofmt`-clean and package-oriented; keep handlers thin and push logic into `internal/*` services. React/TypeScript uses the existing Vite + ESLint setup, 2-space indentation, PascalCase for components (`SeriesHeader.tsx`), and `useX` for hooks (`useReaderPreferences.ts`). Prefer small, behavior-preserving refactors over broad rewrites.

## Documentation & Comments
注释与文档只写**当前的结果**；历史归 `CHANGELOG.md` 与 `docs/adr/`。每段内容有唯一**归属**，环境（`package.json`、`config.example.yaml`、`--help`）本身也是一处。引用代码写符号名——行号、出现次数、代码行数都会**腐坏**。分**层**写，每层只答一个问题，答不下就往下沉：Go 是 package doc → 文件头（≤5 行）→ 符号 doc → 行内，前端去掉 package doc 这层。文件头可选——说不出一句从文件名与 package doc 推不出来的话，就不写。

写文件头、处置带历史的旧注释、判断一段内容归哪个文件时，见 `docs/agents/doc-style.md`；`scripts/check-doc-style.sh` 是配套检查器。

## Testing Guidelines
Add or update `_test.go` files in the touched backend package, following the existing table-driven style in `internal/api/*_test.go` and `internal/scanner/*_test.go`. Frontend changes must pass `npm run test` (vitest) and `npm run build`. If you change SQL in `sql/query.sql` or `schema.sql`, regenerate the Go bindings with `sqlc generate` before testing. If you change a Go response struct that is a generated frontend contract (see `cmd/tsgen`'s `targets`, e.g. `api.TaskStatus`), regenerate `web/src/api/generated.ts` with `go run ./cmd/tsgen` (CI checks it for drift).

> **sqlc 必须在 PowerShell（或 cmd）里运行，不要用 Git Bash / MSYS。** `sqlc generate` 会向 stderr 打印一批 `mismatched input ...` 诊断——这些是非致命噪音，sqlc 会自行恢复并正常生成。在 PowerShell 下命令退出码为 0、产物与仓库一致；但在 Git Bash 下经 scoop shim 调用会返回**假的退出码 1**，看起来像“管线坏了”，其实文件没问题。判断 sqlc 是否成功以 PowerShell 的退出码和 `git status`（有无产物 diff）为准。

## Commit & Pull Request Guidelines
Recent history uses Conventional Commit prefixes such as `feat:` and `fix:` with short imperative summaries. Keep commits scoped to one change set. For user-visible changes, update `CHANGELOG.md` in the same batch. Pull requests should describe the behavior change, list verification commands run, and include screenshots for UI work.

## Agent skills

### Issue tracker

Issues and specs live as markdown files under `.scratch/<feature-slug>/` in this repo, not on GitHub. See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical triage roles, used verbatim as the `Status:` value in each issue file. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` plus `docs/adr/` at the repo root. See `docs/agents/domain.md`.

## Configuration & Runtime Notes
Runtime config lives in `config.yaml`, which is gitignored and never committed (it may hold secrets such as `llm.api_key`). `config.example.yaml` is the tracked template; the server also generates a default on first run. `README.md` documents the flags and env vars that override the config path and log directory — both resolve to absolute at startup, and the database and cache locations come from `database.path` / `cache.dir`. Generated runtime data belongs in `data/` and should not be committed. When editing runtime config flows, `config.Manager` is the source of truth rather than mutating copied snapshots inline.
