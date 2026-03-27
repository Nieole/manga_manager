# TODO

## P0 Stability

- [x] Make runtime configuration access thread-safe instead of sharing a mutable `*config.Config`.
- [x] Support rebuilding the Bleve index without requiring an application restart.
- [x] Add regression tests for runtime config updates and search index rebuild behavior.

## P1 Maintainability

- [ ] Break up oversized frontend pages/components (`Layout`, `Home`, `SeriesDetail`, `BookReader`) into smaller hooks and view components.
- [ ] Expand backend test coverage for scanner, API handlers, and reading progress flows.
- [x] Replace ad hoc background task handling with a clearer task lifecycle and status model.

## P2 Performance

- [x] Avoid full-file log scans for common log queries by keeping only the latest matching entries in memory during scans.
- [x] Revisit thumbnail rebuild and global scan task scheduling to reduce duplicate work.
- [x] Audit file watcher behavior for nested directories and large library churn.
