# Ticket 06 validation

## Implemented behavior

Explicit plan-bound anomaly acceptance uses the existing relocation commit and exclusive-access boundary. Missing, unreadable, non-regular and size-mismatched books all retain their IDs and map to the target root. Invalid roots, occupancy conflicts and changed facts remain blocking.

The same SQLite transaction writes paths, accepted facts, original scan settings and persistent library protection, then disables effective automatic scanning. A later clean relocation keeps protection and the first original settings while recording its current operation and facts. Protection is independent of task history and effective scan mode.

The direct Scanner cleanup seam reads protection before filesystem work and uses conditional book/series deletes so protection established concurrently cannot be bypassed by a check-then-delete window. Explicit user deletion remains separate. Automatic scans, watcher registration, resumed series scans and ordinary scan-mode editing cannot restore automatic processing. Manual scans retain protection. The library menu, relocation result and reopened plan display protection, original settings and consequences in both locales.

## Behavior tests

Controller HTTP tests in `relocation_protection_test.go` cover explicit versus absent consent, all four anomaly kinds, stale target facts and original settings, both missing-book and missing-series association preservation, ordinary editing, cleanup triggers, manual global scans, history pruning and fresh SQLite plus freshly loaded config/Manager startup. Protection-write failure rolls back paths and settings; derived-effect failure leaves committed paths blocked until recovery. Existing normal relocation and unprotected cleanup regressions remain applicable.

The TDD slices were implemented against Controller HTTP, real SQLite/temp config and direct Scanner cleanup. Existing resumed/scheduled/editing bypass assertions, repeat clean migration and protection consent UI tests were observed failing before their corresponding minimal fixes. The final retry of the added anomaly/failure-boundary tests passed with the complete relocation API suite (8.118s).

A relevant race run additionally found a pre-existing shared commit-handler capture race: its task closure read `op.ID` while the handler refreshed `op` for the response. The fix captures an immutable operation ID before task dispatch. The existing relevant race suite then passed for API (89.420s).

## Verification

Go 1.26.5, CGO/GCC, repository-local temporary files and the primary shared Go cache were used. No security setting changed. The first `go test ./...` command did not exit zero: an existing normal-relocation fixture hit Windows `os.Rename: Access is denied`, and Smart App Control blocked scanner execution. Every other package passed. A bounded ordinary rerun of the complete API and scanner packages passed (79.618s and 21.629s). This is full package coverage across runs, not a claim that the original full command exited zero.

`go test -race` for relocation/protected-library/cleanup regressions passed API after the capture fix. Scanner passed the earlier identical-source race run (12.109s); its later combined run was blocked by Smart App Control, so a separate ordinary retry was requested.

- `gofmt -l ./cmd ./internal`: empty.
- `go run ./cmd/tsgen`: 23 contracts, generated file hash unchanged.
- `golangci-lint run --allow-parallel-runners`: exit 0, `0 issues.` after the capture fix. An initial invocation had a process PATH/Go version mismatch; corrected process-local PATH uses the same Go 1.26.5 toolchain.
- Frontend Vitest: 78 files, 843 tests passed.
- Frontend ESLint and own production build: passed.
- After the final bilingual preview hint adjustment: the locale grammar and relocation modal suites passed (202 tests), and own production build passed again.

Actual browser evidence and final documentation gate are recorded separately when complete. Ticket 07 owns the only explicit protection release flow; directory policies remain blocked until ticket 08 extends the common commit/recovery boundary.

Final documentation gate: `scripts/check-doc-style.sh` exited 0. The separate ordinary scanner race retry passed (7.887s). Relevant API and scanner race coverage therefore passed across runs after the commit-handler fix; the combined invocation itself remained nonzero due to the scanner SAC block.

The root agent captured real HTTP/Vite acceptance at `cfc49a7`: `screenshots/anomaly-details.png` and `screenshots/anomaly-protected.png`. An independent fake library retained its original series and both books while one target book was absent; consent was required, and completion displayed retained interval/60/cbz settings with effective automatic mode disabled. The screenshots predate the immutable-ID race fix and the final preview hint correction; see the screenshot README for exact provenance.

## Series automatic-admission review fix

The review found a second check/admission window in non-manual series scans. A deterministic Controller test wraps the existing store boundary: it pauses the second `GetSeries` call after the initial protection check, completes a real anomalous relocation, then resumes admission. Before the fix, the previously checked resumed scan was admitted with a nil error; the test failed. The series RunSpec now re-reads the current series ownership and persistent protection through `ValidateAccess`, inside the engine admission mutex. A changed library owner is rejected, and manual scans retain their existing behavior.

The final barrier regression passed in 5.134s after one normal Smart App Control retry, including the public task-list assertion that no series run was created. The initial green version without that additional run-list assertion passed in 5.243s. Formatting was clean and lint returned exit 0 / `0 issues.` after the production fix. Full API and relevant race reruns are recorded below when complete.

The post-admission-fix relevant API race suite passed (89.454s). Its complete API rerun had one failure: `TestRelocationAnomalyConsentExpiresWhenOriginalSettingsChange` received `relocation_plan_not_ready` at admission. All other API tests passed in that run. That stale-settings case then passed 20 ordinary repeated executions (5.651s). The observed failure and a suspected pre-existing preview reconciliation check/write window were reported to the merger; this is not recorded as a zero-exit complete API rerun. The series admission production fix itself passed the deterministic HTTP/task-list regression and the relevant race suite.
