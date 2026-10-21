# 05 validation: repair an edited library root

## Behavior

- The administrator source-root hints endpoint reads stored series and book paths in bounded batches and returns at most six candidates. It never accesses the original disk or treats a common ancestor as historical proof.
- A library with records outside its current root exposes explicit original-root input. Candidates remain unselected. The preview displays the current library root, mapping source and destination; changing either editable mapping root disables confirmation of the old plan.
- Persisted plans reopen with their confirmed roots. If the library root has subsequently changed beyond both roots of the previous operation, that historical operation does not choose the source of a new repair.
- Repair uses the existing check, confirmation, exclusive access, database transaction, operation recovery and cache invalidation. No replacement endpoint or scan-based identity matching was added.

## Evidence

- `TestRelocationRepairEditedRootRequiresExplicitSourceAndPreservesReading` first failed because the hints endpoint was absent, then passed. A real CBZ is moved, only the library root is changed in the historical fixture, and the original disk location disappears. The current-root preview blocks; explicit original root succeeds even though the library root already equals the target. Duplicate confirmation returns the same operation, both users retain progress and the original book ID serves the expected page bytes.
- `TestRelocationRepairLooseRootBooksAndVirtualSeries` covers a series equal to the original root, a nonexistent virtual series directory and a loose root book.
- `TestRelocationRepairMixedAndPrefixLookalikeRecordsRemainUntouched` rejects mixed old/new records and a sibling with a similar path prefix, and verifies all paths and configuration remain unchanged.
- `TestRelocationRepairPreservesSeededMetadataAndUserAssociations` reuses the full real-data fixture for titles, locks, covers, fingerprints, tags/authors, relations, proposals/provenance, collections/lists, multi-user progress/bookmarks/activity/reviews/reading time. The same IDs and all seeded association rows survive repair; archive and custom-cover bytes remain unchanged.
- The HTTP session/CSRF permission regression now includes the source-root hints endpoint. The broad host-path route sweep covers it as well.
- Modal interaction tests cover explicit candidate selection, no inferred single-series source, stale source edits, persisted roots and the later-root-edit scenario. The first two new interaction tests were observed failing before the UI implementation.

## Checks

- Go 1.26.5: the initial `go test ./...` passed. After adding the complete association repair case and rebuilding frontend assets, the API suite passed again. Windows Smart App Control blocked four unchanged package test executables during that full run; individual normal retries of `cmd/server`, `internal/diskwork`, `internal/logger` and `internal/scanner` subsequently passed. No antivirus exclusions or security settings were changed. The final full command itself returned failure from those execution blocks; aggregate passing results are reported separately from that command exit status.
- `go test -race ./internal/api -run 'TestRelocationRepair|TestRelocationCommitPreservesSeeded|TestRelocationPreviewRequiresAdmin' -count=1` passed.
- `gofmt -l ./cmd ./internal` produced no output; `golangci-lint run --allow-parallel-runners` returned `0 issues.` with a worktree-local lint cache.
- `go run ./cmd/tsgen` regenerated the source-root hints contract.
- Frontend lint, 841 tests in 78 files and the production build passed.
- `scripts/check-doc-style.sh` passed; existing historical-word notices remain advisory.

Actual UI screenshots are collected by the integration owner against a dedicated temporary demonstration library; test fixture setup must not touch a user's library database.
