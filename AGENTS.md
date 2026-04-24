# Repository Guidelines

## Project Structure & Module Organization
`cmd/server/main.go` is the backend entrypoint. Core Go packages live under `internal/`:
`api/` for HTTP handlers, `scanner/` for library discovery, `metadata/` for scraping and LLM prompts, `database/` for SQLite and generated `sqlc` code, and `parser/`, `images/`, `koreader/`, `search/`, `config/`, `logger/` for supporting services. SQL sources are in `sql/query.sql` and `internal/database/schema.sql`. The Vite frontend lives in `web/`; routes are in `web/src/App.tsx`, pages in `web/src/pages/`, shared UI in `web/src/components/`, and locale/theme helpers in `web/src/i18n/` and `web/src/theme/`. Screenshots and marketing assets are under `images/`.

## Build, Test, and Development Commands
Run the frontend locally with `cd web && npm run dev`. Build the frontend with `cd web && npm run build`; this is the standard verification step for UI changes. Lint the frontend with `cd web && npm run lint`. Run backend tests with a repo-local Go cache to avoid sandbox/cache permission issues:
```bash
GOCACHE=/Users/nicoer/dev/manga_manager/.gocache GOTMPDIR=/Users/nicoer/dev/manga_manager/.tmp go test ./...
```
Use `./build.sh` for a full release-style build; it installs frontend dependencies, builds `web`, and cross-compiles binaries into `build/`.

## Coding Style & Naming Conventions
Go code should stay `gofmt`-clean and package-oriented; keep handlers thin and push logic into `internal/*` services. React/TypeScript uses the existing Vite + ESLint setup, 2-space indentation, PascalCase for components (`SeriesHeader.tsx`), and `useX` for hooks (`useReaderPreferences.ts`). Prefer small, behavior-preserving refactors over broad rewrites.

## Testing Guidelines
Add or update `_test.go` files in the touched backend package, following the existing table-driven style in `internal/api/*_test.go` and `internal/scanner/*_test.go`. Frontend changes should at minimum pass `npm run build`. If you change SQL in `sql/query.sql` or `schema.sql`, regenerate the Go bindings with `sqlc generate` before testing.

## Commit & Pull Request Guidelines
Recent history uses Conventional Commit prefixes such as `feat:` and `fix:` with short imperative summaries. Keep commits scoped to one change set. For user-visible changes, update `CHANGELOG.md` in the same batch. Pull requests should describe the behavior change, list verification commands run, and include screenshots for UI work.

## Configuration & Runtime Notes
Runtime config lives in `config.yaml`. Generated runtime data belongs in `data/` and should not be committed. When editing runtime config flows, remember `config.Manager` is the source of truth rather than mutating copied snapshots inline.
