# 07 validation: explicit restoration of protected libraries

## Behavior and boundaries

Administrators can read the durable original protection settings and accepted historical anomalies separately from a current, read-only restoration check. Checks reuse bounded relocation inspection and details, carry an explicit restoration purpose and protection generation, remain cancellable, and survive modal closure and run-history pruning. A clean check preserves protection; ordinary migration confirmation cannot consume a restoration check.

Explicit release binds a server-created check to current library roots, records, automatic settings, protection generation, policy configuration and filesystem facts. Remaining anomalies require separate acknowledgment of the possible later deletion of missing records and associated data. Administrators may choose none, interval or watch with normalized formats and a positive interval. Repeated and concurrent confirmation returns one durable result without overwriting later ordinary edits.

The release uses unified library exclusivity and rejects related active or queued work and read access. Its durable pending decision preserves protection and effective library mode none. Watcher registration uses the selected settings in the operation. Only after derived effects succeed does one SQLite transaction publish selected automatic settings, remove the matching protection generation and persist completion, guarded by the current root. A settings, protection or completion-write fault rolls back that transaction. Failed effects keep access reserved and protection active; fresh controller assembly reserves unfinished decisions before replaying effects. Release changes no paths, records or archive bytes and launches no immediate scan or cleanup. Later ordinary cleanup may remove missing records after an explicitly accepted release.

Configuration coordination reads actual disk policies, detects unpublished external edits, and verifies disk bytes around the durable decision and effects without writing configuration. Task labels and results use opaque IDs, purpose and localization codes. Restoration task failures retain diagnostic errors in contextual logs and return a fixed task error instead of host paths.

## Evidence

`relocation_restore_test.go` exercises Controller HTTP with real SQLite, native temporary directories and persisted configuration. It covers clean and remaining-anomaly release, absent consent, changes to file existence or mtime, record size, roots, interval, formats, protection generation and disk-only policies. It verifies duplicate confirmation after a later edit, concurrent confirmation producing one operation/run, related read and active/queued work rejection, unrelated reading, bounded 100-item details with a 105-anomaly fixture, cancelled checks and pruned history.

The failure matrix injects SQLite faults at the pending decision, selected settings, protection deletion and completion result. A real watcher-registration failure caused by taking the root offline retains protection and mode none. Fresh SQLite connections, a Manager loaded from the real file, and production controller assembly preserve the blocked result across history pruning and an offline restart; a subsequent available-root restart atomically publishes the chosen watch settings and completes. Direct Scanner cleanup remains protected throughout recovery.

The combined legacy repair, missing-book and mapped-policy qualification is extended in `relocation_combined_test.go`: recheck alone retains protection, omitted remaining-risk consent returns 409, explicit selected interval settings survive another fresh restart, and paths, IDs, two-user progress, collection membership, fingerprints, archive bytes and policy configuration remain unchanged. Only subsequent explicit ordinary cleanup deletes the missing records.

The TDD evidence includes a missing endpoint returning 404 before the first complete release slice, a disk-only policy edit incorrectly completing before freshness coordination, a watcher error exposing a host path in task data before the safe adapter, related queued work being admitted before the preflight guard, and pending watch settings becoming visible before the final atomic-publication change. The relevant race gate exposed an existing normal-check closure capture of a mutable response plan; both check adapters capture immutable plans and the release adapter captures an immutable operation ID.

Frontend coverage includes explicit clean release, remaining-risk consent, settings invalidation, cancellation, stale results, generation changes, persistence/recovery, cross-library late responses, historical/current pagination, protected-library entry, exact restoration links, ordinary-modal purpose rejection and real Chinese/English rendering. Actual browser screenshots and precise source provenance are recorded in `screenshots/README.md`; they use generated CBZ files and an isolated fake library.

## Final checks

The final product is `9e2fdc6`. Go 1.26.5 with CGO/GCC, the primary shared Go cache and this worktree's local temporary/lint caches was used. Smart App Control remained enabled, and no restart or security exception was used. Four unchanged-package execution refusals in the initial full Go attempt passed normal bounded retries; the final complete command itself subsequently exited zero.

- `go test ./...`: exit 0; API 72.342s, database 26.672s, scanner 17.224s, all other packages passed or cached.
- `go test -race ./internal/api -run 'Test(Restoration|Relocation)' -count=1`: exit 0; API 154.840s, including normal, protected, repair, policy, combined and restoration regressions.
- `gofmt -l ./cmd ./internal`: empty.
- `go run ./cmd/tsgen`: 24 types; generated file hash unchanged.
- `golangci-lint run --allow-parallel-runners`: exit 0, `0 issues.` after the final transaction and capture fixes.
- Final frontend Vitest: 80 files, 862 tests passed in this worktree. The English pluralization regression is fixed using normal plural templates and a stable time unit, without changing grammar rules or exemptions.
- Frontend ESLint: exit 0, no warnings. Own production build: exit 0.
- `scripts/check-doc-style.sh`: exit 0; existing historical-word notices are advisory.
