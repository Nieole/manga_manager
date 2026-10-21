# 08 validation: directory policy durability

## Behavior and boundaries

Explicit policies at the mapping source and its descendants follow the same relative-path mapping. Equal target entries deduplicate; unequal entries and changed behavior at another library root or descendant policy boundary block confirmation. Ancestor entries and unrelated settings retain their values.

Policy migration uses the shared confirmation service and configuration update boundary. A private policy-only journal precedes configuration replacement; SQLite paths and the committed decision share one transaction. Prepared work compensates, committed work rolls forward, and divergent policy facts retain a scoped access restriction. Recovery reserves every affected library before configuration updates can release queues, and holds the update boundary through runtime publication and derived effects. Configuration hot reload starts after controller recovery.

## Evidence

Controller HTTP tests use SQLite, native temporary directories, real CBZ files and persisted configuration. They cover root/child mapping, target deduplication, unchanged nested libraries, unrelated settings saved after preview, SQL abort and subsequent valid confirmation, configuration persistence failure before and after replacement, runtime failure after SQL commit, and fresh Manager recovery through the production controller constructor.

The subprocess harness exits at the actual atomic configuration replacement, runtime application and derived-effect boundaries. New managers load the real file and recover twice without reapplying path mappings or losing unrelated settings. Recovery evidence survives run-history deletion and repeated failed restarts. Owned entries, default profile, default IO values and ancestor policy drift remain blocked until explicitly corrected; the journal stores only directory policies and their defaults, never complete configuration or credentials.

Barrier-controlled browser saving and Reload share the migration publication boundary; stale browser updates return 409. External edits during runtime application or derived effects retain access restrictions and preserve edited bytes. Failed ordinary Save/Reload compensation also preserves a concurrent external editor's newer file. The latter two regressions were observed failing before their fixes. Source duplicate policy ambiguity and ancestor drift were likewise observed RED before implementation.

UI tests preserve the single result view for prepared and paths-committed states and prevent replacing a recovery operation with a new check. The real UI evidence is supplied by the integration owner.

## Checks

- Frontend lint, 842 tests in 78 files and the production build passed after merging ticket 05.
- `go run ./cmd/tsgen` produced 22 types with no tracked drift.
- `gofmt -l ./cmd ./internal` was empty; compatible Go 1.26.5 `golangci-lint run --allow-parallel-runners` returned `0 issues.`.
- The final full Go command had Smart App Control execution refusals for API, scanner and taskstore. A normal retry passed the full API suite (70.219s); a further normal retry passed scanner (17.554s) and taskstore (3.598s). Every other package passed in the full run. This establishes aggregate package coverage; the full command itself returned failure. No security settings changed.
- Final go test -race ./internal/api ./internal/config ./internal/relocation -run 'TestRelocation|TestConfigFailedUpdate|TestManager' -count=1 passed: API 93.244s, config 2.708s.
- scripts/check-doc-style.sh passed; existing historical-word notices remain advisory.

## Review follow-up

The zero-policy plan path also reads and validates actual disk policy facts before preparing its durable decision. It retains the policy-only journal without rewriting configuration, verifies disk bytes before SQL commit and after derived effects, and participates in the same restart recovery. `TestRelocationZeroPolicyPreviewRejectsUnpublishedDiskPolicy` was observed RED (completed with a disk-only source-child policy edit) and then GREEN, preserving both database paths and the un-reloaded external policy.

After this fix, the focused policy HTTP suite passed (6.729s), the complete relocation/config-compensation race selection passed (91.843s), lint returned `0 issues.` and gofmt output remained empty. No frontend source changed.