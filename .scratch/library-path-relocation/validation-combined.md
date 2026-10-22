# Combined relocation acceptance

`TestRelocationCombinedLegacyRepairProtectionAndPolicyRecovery` qualifies the combined HTTP, real SQLite, filesystem, saved configuration, fresh Manager startup and direct Scanner boundaries. It runs against the integrated 05/06/08 behavior, with normal completion and a runtime configuration application failure after paths commit.

The fixture has a library root edited before repair, unavailable old source, a readable archive, one missing book, and a missing series retained in a collection. Explicit source selection produces one anomaly and two mapped IO policies. A separate settings save after preview remains intact. Confirmation accepts the anomaly in the same operation that maps paths and policies and disables automatic processing.

Both variants close and reopen SQLite, load a new Manager from disk, and enter the production controller recovery assembly after task history removal. They verify completed operation state, readable original book ID, durable protection, direct Scanner cleanup rejection, preserved original automatic settings, independent reader progress, collection membership, fingerprints, unchanged archive bytes, and matching disk/effective policies. The failure variant also verifies HTTP 503 before recovery, preventing mixed-state reads.

Validation on integration `ebff4400e117d87fd71092b459379810c3689d60` plus this test:

- `go test ./internal/api -run '^TestRelocationCombined' -count=1`: passed, 5.032s before the final 06 closure merge.
- `go test -race ./internal/api -run '^TestRelocationCombined' -count=1`: passed, 9.683s after the final 06 closure merge.
- `golangci-lint run --allow-parallel-runners`: 0 issues.
- `gofmt -l ./cmd ./internal`: no output.

The same two scenarios also qualify ticket 07 after migration recovery. An authenticated administrator checks current facts without clearing protection, receives HTTP 409 when remaining-loss-risk consent is omitted, then explicitly releases protection with selected interval settings. Release creates no scan or cleanup task. A second SQLite reopen and fresh Manager startup retain the selected settings, original missing-record IDs, paths, user progress, fingerprints, collection membership, and exact migrated disk/effective configuration. Only a subsequent explicit ordinary cleanup deletes missing records; the readable book still serves its original page.

Additional validation after normal merges of integration `9ebb037` and ticket 07 backend `ad8d9bb57801ee6e07600e5d427cef0b4a28e7b3`:

- Extended combined test: passed, 7.305s on the first stable 07 contract.
- Extended combined test with `-race`: passed, 14.709s after the 07 disk freshness and watcher recovery fixes.
- `golangci-lint run --allow-parallel-runners`: 0 issues.
- `gofmt -l ./cmd ./internal` and `git diff --check`: no output.

No Smart App Control retry was needed for these executions. This qualification adds tests only; ticket owners retain their independent failure and concurrency matrices.
