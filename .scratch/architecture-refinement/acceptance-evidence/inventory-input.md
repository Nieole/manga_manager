# READ-ONLY draft evidence inventory: approved US001–US204

Observed integrated HEAD: `7589b153c9a197517d559d4e63b579c498e22ce7` (`codex/architecture-refinement`). Initial worktree status was clean. Read-only source/issue inspection; no tests, builds, generators, dependency installs, repository writes, or index/history mutations performed. Only this report and its external helper are written under Windows Temp.

## Scope and status discipline

- Ground truth read: root AGENTS.md and CONTEXT.md; architecture-refinement spec.md (all 204 stories, decisions, tests and exclusions), design.md, queue.md, execution.md; all 48 issue summaries/checklists/comments. Spec-graph search returned no matches for this unindexed scratch specification; raw approved documents were used.
- Ticket48 actual file: `.scratch/architecture-refinement/issues/48-integrated-acceptance.md`. It requires every-story implementation/consumer/execution/manual-ownership mapping and sends missing functionality back to its owner ticket. It does not authorize scope expansion or filling predecessor omissions inside48.
- Parent operational state governs:42 closed;43/44/45 integrated;46 active isolated migration/merge;47 not started;48 blocked. Some issue Status fields and execution.md prose still say43/44/45 ready or pending; these are stale documentation, not authority to redo their code. Current HEAD is the45 integration merge. No46 worktree was accessed or altered.
- **O+T** = observed implementation owner + actual production consumer boundary + existing regression symbol pointer. It means SOURCE EVIDENCE ONLY, not test PASS, not exhaustive proof of that story. Group references in each row resolve to explicit source and consumer symbols in the capability catalogue below.
- **P46/P47** = a documented remaining migration/contraction obligation, even where underlying behavior has O+T evidence. **M48** = missing final acceptance evidence/manual audit, not necessarily missing production behavior. **U** = execution unperformed, especially real hardware/provider/platform. A narrow representative test never establishes every clause or whole-repository structural ownership.
- No current-tree full-pass assertion. Historical execution.md/issue/log references below are historical scoped claims; exact raw-log existence/content was not audited. Historical filenames and counts are not rerun evidence for HEAD.
- Coverage depth: all204 intents and48 issue summaries were indexed. Deep source/test reads concentrated on delete/drain, transfer, publication, proposal concurrency/refresh, password handoff, bootstrap lifecycle and offline consumer facts. Other rows are symbol/source-consumer inventory with representative tests, NOT a line-by-line correctness audit; their final clause-specific manual review remains M48.

## Highest-priority material consumer evidence for48 to verify

1. **Pending predecessor boundaries (US001–014,044,151–152,163,184,192,202):** consume reviewed46 integration, then47 removes the old Controller constructor/service mirrors and `*_http_compat.go` forwarding surface. Bootstrap/Host still retains `Transport.legacy *Controller` and `database.Store` in NewTransport at observed HEAD. This is expected pending47, not a48 implementation ticket. Verify all admin/regular/protocol routes survive contraction with the same Engine, Manager, auth, SSE and cache.
2. **One real production shutdown consumer (US154,197):** broker source tests cover live and post-stop registration; bootstrap tests cover a blocked ordinary HTTP request and resource-using run. Inspected bootstrap suite does NOT combine a routed live `/api/events` connection, active content/whole-book reader, `Process.Shutdown` with real `http.Server.Shutdown`, and resource retirement. Material missing joint witness: establish SSE is cut before HTTP drain would wait indefinitely, normal request/body exits before Engine/resources retire. Reuse current seams; no universal lifecycle framework.
3. **Committed result -> refresh/recovery consumers (US041–043,104,119,178):** proposal/detail tests now cover failed authoritative refresh and query-only retry (prior gap fixed). Verify remaining actual consumers: bulk metadata later failure, mark committed IDs, ComicInfo file committed but fact-store warning -> next HTTP read, and external terminal/reconnect after partial copy/projection failure. Existing owner tests prove parts; do not assume a toast or task terminal covers every post-commit consumer. Record original committed range and no automatic rewrite.
4. **Account/auth identity after46/47 (US184–193,166–168,183):** real SQLite use-case/HTTP tests already cover concurrent admin demotion/delete, fresh actor authorization, setup race, exact username, old in-flight Basic cache refill, committed password/replacement-session failure. Frontend tests already cover expiry/logout/owner/late ACK. Missing evidence is final bound-Host route/security parity AFTER migration and cross-boundary identity transition, not absence of all these rules. Verify cookie/CSRF/Basic/device challenge paths retain distinct responsibilities.
5. **Actual restart repair + stale consumers (US061–071,136,153):** durable reopen/version tests and runtime repair tests exist. Verify final production assembly recovery-before-open and immediate-retry view consume same durable responsibility, including old failure after newer success and cascade/name changes. Do not infer restart coverage from in-memory retry alone.
6. **Resource/physical file acceptance (US079–088,098–122,196):** existing deterministic checkpoints cover actual body exit vs terminal snapshot, queue reservation, cancellation, pause quota release, no-clobber publication, source/staging changes. Need final combined run plus Linux/macOS real execution (Windows historical failures and symlink skips remain limitations), real-device/provider/hardware limitations explicitly retained. OS-blocked IO interruption and cross-process transfer resume are not requirements.
7. **Offline fact producer/server consumer join (US026,166–168):** producer tests exercise original timestamps, stable fact IDs, delayed ACK across owner changes and lost ACK retry. Backend reading and KOReader tests exercise reject-old across clear/reopen and committed position vs later protocol-store failure. A single browser-to-live-server restart/clear roundtrip is not established here; verify protocol payload parity with existing contract/HTTP tests rather than demand a new browser automation framework. Actual device storage/service-worker behavior is U.
8. **Source-vs-execution closure (US194–204):** final majority-area keep/change/verify audit, removed-authority search, ADR/current-doc and CHANGELOG reconciliation, generated-contract drift, full suitable gates on final integrated tree and independently scoped failures/timeouts. This external note is not completion evidence. A source row with a test pointer cannot be promoted to PASS.

## Capability catalogue (required resolution of each row’s group)

### A — Whole-project ownership / contraction

Implementation owner(s):
- `internal/bootstrap/bootstrap.go` — `Open`, `Close`, `Shutdown`, `Options`, `Process`
- `internal/api/host.go` — `NewTransport`, `RuntimeEffects`, `TaskOptions`, `Bind`, `SetupRoutes`, `SetupOPDSRoutes`, `SetupKOReaderRoutes`, `Lifecycle`, `ReadingDependencies`, `RunDependencies`, `LibraryDependencies`, `AdministrationDependencies`, `HostDependencies`, `Transport`, `Host`
- `internal/api/controller.go` — `NewController`, `Close`, `Shutdown`, `PublishEvent`, `SetupRoutes`, `setupRoutes`, `ShutdownNotify`, `Controller`, `TaskRuntime`, `SystemCapabilitiesResponse`, `SystemConfigResponse`, `SearchResult`, `SearchHit`, `controllerCacheSizes`, `BulkUpdateSeriesRequest`

Actual consumers / regression consumers:
- `cmd/server/main.go` — module exports/types (see source)
- `internal/api/host.go` — `NewTransport`, `RuntimeEffects`, `TaskOptions`, `Bind`, `SetupRoutes`, `SetupOPDSRoutes`, `SetupKOReaderRoutes`, `Lifecycle`, `ReadingDependencies`, `RunDependencies`, `LibraryDependencies`, `AdministrationDependencies`, `HostDependencies`, `Transport`, `Host`

Historical record lookup: execution.md / issue 42; 43/44 combined; 45 issue evidence; 47 pending.

### R — Reader-bound book facts and navigation

Implementation owner(s):
- `internal/reading/book_view.go` — `AnonymousSubject`, `UserSubject`, `BookView`, `SeriesBooks`, `AdjacentView`, `UserID`, `Inspect`, `Subject`, `BookFacts`, `ReaderState`, `BookSeriesView`
- `internal/reading/queries.go` — `NewQueries`, `BookQueryStore`, `Queries`, `Continue`, `RecentStore`
- `internal/reading/recent_view.go` — `RecentBooks`, `RecentBooksView`

Actual consumers / regression consumers:
- `internal/api/reader_http.go` — `NewReaderCapabilities`, `ReaderStore`, `ReaderCapabilities`, `ReaderHTTP`
- `internal/bootstrap/reader_http_test.go` — consumer fixture/test suite, not production owner

Historical record lookup: execution.md / issue 01/04/07/43.

### Q — Reader-aware SQL catalogue / cache

Implementation owner(s):
- `internal/reading/catalogue.go` — `NewCatalogueQueries`, `SeriesCatalogueStore`, `CatalogueQueries`
- `internal/reading/catalogue_view.go` — `Search`, `Recent`, `CatalogueFilters`, `CatalogueSearch`, `CataloguePage`, `RecentSeriesView`
- `internal/catalogsearch/search.go` — `New`, `Query`, `Store`, `Search`, `Result`, `Hit`
- `internal/seriescontext/context.go` — `Error`, `Unwrap`, `New`, `Load`, `Store`, `Relations`, `Proposals`, `Runs`, `Result`, `AuxiliaryError`, `Service`
- `internal/recommendation/recommendation.go` — `Key`, `Load`, `Item`, `Store`
- `internal/dashboard/dashboard.go` — `New`, `InvalidateAll`, `InvalidateVolatile`, `LoadGlobal`, `Load`, `cachedStructuralStats`, `cachedVolatileStats`, `Service`, `Store`

Actual consumers / regression consumers:
- `internal/api/reader_http.go` — `NewReaderCapabilities`, `ReaderStore`, `ReaderCapabilities`, `ReaderHTTP`
- `internal/bootstrap/reader_http_test.go` — consumer fixture/test suite, not production owner
- `web/src/pages/library/hooks/useLibrarySeries.ts` — `useLibrarySeries`

Historical record lookup: execution.md / issue 02/41/43.

### W — Reading writes / real commit ranges

Implementation owner(s):
- `internal/reading/position.go` — `New`, `WithClock`, `RecordPosition`, `Position`, `Result`, `Store`, `Service`, `Option`
- `internal/reading/mark.go` — `MarkBooks`, `MarkSeries`, `MarkBooksInput`, `MarkSeriesInput`, `MarkResult`
- `internal/reading/sync.go` — `SyncPositions`, `SyncResult`
- `internal/reading/time.go` — `RecordTime`, `TimeReport`
- `internal/reading/bookmarks.go` — `NewBookmarks`, `List`, `Save`, `Delete`, `BookmarkStore`, `Bookmarks`, `BookmarkInput`, `Bookmark`

Actual consumers / regression consumers:
- `internal/api/reader_http_compat.go` — `SetupOPDSRoutes`
- `web/src/session/catalogConsumers.test.tsx` — consumer fixture/test suite, not production owner

Historical record lookup: execution.md / issue 05/06/34.

### M — Manual and selection metadata

Implementation owner(s):
- `internal/application/seriesediting/manual.go` — `New`, `Save`, `Transaction`, `Store`, `Result`, `Service`
- `internal/application/seriesediting/selection.go` — `NewSelection`, `Edit`, `SelectionTransaction`, `SelectionStore`, `SelectionResult`, `SelectionService`
- `internal/application/seriesediting/sql.go` — `NewSQLStore`, `Edit`, `EditSelection`, `RefreshSeriesStats`, `SQLTransactions`, `sqlStore`, `SQLStore`
- `internal/seriesmeta/manual.go` — `SaveManual`, `ManualQueries`, `Author`, `Link`, `ManualInput`
- `internal/seriesmeta/bulk.go` — `EditSelection`, `SelectionQueries`, `SelectionInput`

Actual consumers / regression consumers:
- `internal/api/organization_http.go` — `setupRoutes`, `CollectionsCapability`, `ShelvesCapability`, `ReadingListsCapability`, `RelationsCapability`, `GroupingCapability`, `MetadataEditor`, `SelectionEditor`, `ProposalCapability`
- `web/src/features/series-metadata/useSeriesEdit.ts` — `useSeriesEdit`

Historical record lookup: execution.md / issue 08/09/44.

### P — Proposal transaction-current adjudication

Implementation owner(s):
- `internal/proposal/resolve.go` — `Apply`, `Reject`, `ApplyMode`, `ApplyStatus`, `ApplyResult`, `RejectStatus`, `RejectResult`
- `internal/proposal/service.go` — `NewService`, `Queries`, `Database`, `Service`
- `internal/seriesmeta/proposal.go` — `ApplyProposal`, `ProposalQueries`, `ProposalInput`

Actual consumers / regression consumers:
- `internal/api/metadata_review_controller.go` — `metadataReviewFieldView`, `metadataReviewView`, `metadataProvenanceView`, `metadataReviewResponse`, `metadataReviewInboxItemView`, `metadataReviewInboxResponse`, `metadataReviewBulkRequest`, `metadataReviewBulkResponse`
- `web/src/pages/series-detail/hooks/useSeriesMetadataReview.ts` — `useSeriesMetadataReview`
- `web/src/session/proposalConsumers.test.tsx` — consumer fixture/test suite, not production owner

Historical record lookup: execution.md / issue 10/34 follow-up/44.

### C — Manual collections / smart snapshots / ordered lists

Implementation owner(s):
- `internal/organize/collections.go` — `DerivedCollectionSourceType`, `CollectionNameExists`, `CheckCollectionName`, `NewCollections`, `Get`, `List`, `Members`, `Create`, `Update`, `Delete`, `AddMembers`, `RemoveMember`, `CollectionNameReader`, `CollectionStore`, `Collections`
- `internal/organize/smart_collections.go` — `NewSmartCollections`, `Error`, `Unwrap`, `List`, `Get`, `Save`, `Update`, `Delete`, `Members`, `SmartCollectionPageLimit`, `Views`, `SmartFilter`, `SaveSmartFilter`, `CollectionView`, `SmartCollectionStore`, `SmartCollections`, `InvalidSmartFilter`, `SmartCollectionPage`
- `internal/organize/smart_snapshot.go` — `PreviewSnapshot`, `Snapshot`, `SmartSnapshotRequest`, `SmartSnapshotResult`, `SmartSnapshotPreview`
- `internal/organize/reading_lists.go` — `NewReadingLists`, `List`, `Get`, `Create`, `Update`, `Delete`, `Add`, `Remove`, `Reorder`, `Items`, `Page`, `ReadingListStore`, `ReadingLists`, `ReadingListItem`, `ReadingListPage`

Actual consumers / regression consumers:
- `internal/api/organization_http.go` — `setupRoutes`, `CollectionsCapability`, `ShelvesCapability`, `ReadingListsCapability`, `RelationsCapability`, `GroupingCapability`, `MetadataEditor`, `SelectionEditor`, `ProposalCapability`
- `web/src/pages/readingListsSwitchRace.test.tsx` — consumer fixture/test suite, not production owner

Historical record lookup: execution.md / issue 03/11/15/44.

### AI — Authoritative grouping decisions

Implementation owner(s):
- `internal/organize/ai_grouping.go` — `Error`, `NewAIGrouping`, `Review`, `EditCollection`, `AIGroupingSeriesIDs`, `ApplyCollection`, `ApplyReview`, `RejectCollection`, `RejectReview`, `InvalidAIGroupingEdit`, `AIGroupingStore`, `AIGrouping`, `AIGroupingReview`, `AIGroupingApplyResult`, `EditAIGroupingCollection`, `AIGroupingOutcome`

Actual consumers / regression consumers:
- `internal/api/organization_http.go` — `setupRoutes`, `CollectionsCapability`, `ShelvesCapability`, `ReadingListsCapability`, `RelationsCapability`, `GroupingCapability`, `MetadataEditor`, `SelectionEditor`, `ProposalCapability`
- `web/src/pages/aiGroupingReviewInbox.test.tsx` — consumer fixture/test suite, not production owner

Historical record lookup: execution.md / issue 12/44.

### F — Durable global franchise repair

Implementation owner(s):
- `internal/organize/relations.go` — `NewRelations`, `Create`, `Update`, `Delete`, `Graph`, `LibraryGraph`, `List`, `RelationStore`, `Relations`, `CreatedRelation`, `SeriesRelation`
- `internal/organize/franchise.go` — `Rebuild`
- `internal/organize/franchise_state.go` — `FranchiseState`, `RepairDue`, `RetryFranchiseRepair`
- `internal/application/runtime/lifecycle.go` — `SampleInterval`, `BackoffPolicy`, `Start`, `Shutdown`, `DispatchScheduledScans`

Actual consumers / regression consumers:
- `internal/api/organization_http.go` — `setupRoutes`, `CollectionsCapability`, `ShelvesCapability`, `ReadingListsCapability`, `RelationsCapability`, `GroupingCapability`, `MetadataEditor`, `SelectionEditor`, `ProposalCapability`
- `web/src/features/organize/FranchiseRepairStatus.tsx` — `FranchiseRepairStatus`

Historical record lookup: execution.md / issue 13/14/42/44.

### L — Complete library management and drain

Implementation owner(s):
- `internal/application/libraries/delete.go` — `New`, `Delete`, `Store`, `Watcher`, `Effects`, `DeleteResult`, `Service`
- `internal/application/libraries/save.go` — `Update`, `Error`, `Create`, `CreateInput`, `UpdateInput`, `InitialScanResult`, `SaveResult`, `ValidationIssue`, `ValidationError`
- `internal/task/library_reservation.go` — `ReserveLibrary`, `Upgrade`, `Release`, `Retire`, `LibraryReservation`
- `internal/task/library_drain.go` — `Drain`, `LibraryDrainResult`

Actual consumers / regression consumers:
- `internal/api/library_http.go` — `LibraryQueries`, `ComicInfoQueries`, `libraryHTTP`, `maintenanceHTTP`, `diskHTTP`, `BookRemovalStore`, `removalHTTP`, `relocationHTTP`
- `web/src/features/libraries/useLibraryManagement.ts` — `useLibraryManagement`
- `web/src/features/libraries/DeleteLibraryDialog.tsx` — `DeleteLibraryDialog`

Historical record lookup: execution.md / issue 22/23/24/45.

### B — Shared content source, page and whole-book handle

Implementation owner(s):
- `internal/bookcontent/source.go` — `AcquireRead`, `New`, `Source`, `List`, `ListSource`, `PurgeSources`, `LibraryAccess`, `SourceLookup`, `cachedSource`, `Service`, `Manifest`
- `internal/bookcontent/page.go` — `Page`, `DiskCacheEnabled`, `PageOptions`, `PageResult`
- `internal/bookcontent/file.go` — `Error`, `Unwrap`, `Read`, `Seek`, `Close`, `Open`, `FileError`, `ReadHandle`
- `internal/bookcontent/reader.go` — `NewArchiveReader`, `List`, `Page`, `NamedPage`, `ArchiveBorrower`, `ArchiveReader`, `PageBytes`

Actual consumers / regression consumers:
- `internal/api/library_http.go` — `LibraryQueries`, `ComicInfoQueries`, `libraryHTTP`, `maintenanceHTTP`, `diskHTTP`, `BookRemovalStore`, `removalHTTP`, `relocationHTTP`
- `internal/api/reader_http.go` — `NewReaderCapabilities`, `ReaderStore`, `ReaderCapabilities`, `ReaderHTTP`

Historical record lookup: execution.md / issue 16/17/31/45.

### PUB — ComicInfo stable publication and post-commit warning

Implementation owner(s):
- `internal/bookcontent/publication.go` — `NewPublisher`, `Publish`, `Publication`, `Publisher`
- `internal/parser/comicinfo_writeback.go` — `WriteComicInfoIntoArchive`, `Close`, `Publish`, `Read`, `PrepareComicInfo`, `PreparedComicInfo`, `contextReader`
- `internal/application/diskjobs/comicinfo.go` — `ComicInfoKey`, `WriteSeriesComicInfo`, `WriteComicInfoPrepared`, `Error`, `Unwrap`, `ComicInfoLaunch`, `PreparationError`

Actual consumers / regression consumers:
- `internal/api/library_http.go` — `LibraryQueries`, `ComicInfoQueries`, `libraryHTTP`, `maintenanceHTTP`, `diskHTTP`, `BookRemovalStore`, `removalHTTP`, `relocationHTTP`
- `internal/api/comicinfo_publication_test.go` — consumer fixture/test suite, not production owner

Historical record lookup: execution.md / issue 18/45.

### X — External-root isolation and transfer copy session

Implementation owner(s):
- `internal/external/manager.go` — `NewManager`, `CreateSession`, `GetSession`, `GetSeriesCoverage`, `ScanSession`, `PrepareTransfer`, `MarkTransferred`, `ClearSession`, `InvalidateLibrary`, `Store`, `SessionSnapshot`, `SeriesCoverage`, `TransferOperation`, `TransferPlan`, `seriesEntry`, `session`, `Manager`
- `internal/external/transfer.go` — `Transfer`, `TransferResult`, `TransferFailure`, `TransferProgress`, `TransferWork`
- `internal/external/copy.go` — `CopyFile`
- `internal/application/diskjobs/transfer.go` — `Error`, `Unwrap`, `TransferExternal`, `ExternalTransferKey`, `TransferPrepared`, `TransferLaunch`, `LaunchError`

Actual consumers / regression consumers:
- `internal/api/library_http.go` — `LibraryQueries`, `ComicInfoQueries`, `libraryHTTP`, `maintenanceHTTP`, `diskHTTP`, `BookRemovalStore`, `removalHTTP`, `relocationHTTP`
- `web/src/features/external-library/useExternalLibrary.ts` — `useExternalLibrary`

Historical record lookup: execution.md / issue 19/20/21/45.

### S — Ingestion, rename identity and cleanup qualification

Implementation owner(s):
- `internal/scanner/ingestion.go` — `ingest`, `ingestionPlan`, `archiveDiscovery`
- `internal/scanner/ingestion_writer.go` — `persist`, `ingestionStore`, `ingestionWriter`, `claimedArchive`, `ingestedBatch`
- `internal/scanner/ingestion_coordinator.go` — `scanFinished`, `cleanupAfterScan`, `ingestionCoordinator`, `cleanupSchedule`, `watcherTimings`
- `internal/scanner/library_cleanup.go` — `cleanupStore`, `libraryCleanup`
- `internal/application/scanning/scanning.go` — `New`, `BatchEvent`, `ScanLibrary`, `ScanSeries`, `CleanupLibrary`, `WatchLibrary`, `WatchIngestion`, `WatchCleanup`, `WatchCleanupWhen`, `Store`, `Scanner`, `Effects`, `Service`

Actual consumers / regression consumers:
- `internal/application/runtime/lifecycle.go` — `SampleInterval`, `BackoffPolicy`, `Start`, `Shutdown`, `DispatchScheduledScans`
- `internal/api/library_http.go` — `LibraryQueries`, `ComicInfoQueries`, `libraryHTTP`, `maintenanceHTTP`, `diskHTTP`, `BookRemovalStore`, `removalHTTP`, `relocationHTTP`

Historical record lookup: execution.md / issue 27/28/45.

### I — Neutral identity evidence; device protocol remains separate

Implementation owner(s):
- `internal/bookidentity/evidence.go` — `FingerprintFile`, `FingerprintFileContext`, `Read`, `FingerprintQuickFile`, `FingerprintRelativePath`, `FingerprintPath`, `ContentHash`, `QuickHash`, `PathHash`, `contextReader`
- `internal/bookidentity/rebuild.go` — `New`, `Rebuild`, `Store`, `TaskHandle`, `Service`, `RebuildOptions`

Actual consumers / regression consumers:
- `internal/scanner/ingestion.go` — `ingest`, `ingestionPlan`, `archiveDiscovery`
- `internal/application/maintenance/jobs.go` — `RebuildThumbnails`, `CleanupThumbnails`, `RebuildFileIdentities`, `RebuildFileIdentityRecords`, `BackfillBookHashes`, `ChainBookHashBackfill`, `BackfillFullHashRecords`, `ReportHashProgress`, `Advance`, `RebuildBookHashes`, `ReconcileKOReaderProgress`, `RefreshKOReaderMatching`, `hashingFrameHandle`, `koreaderFingerprintHandle`, `koreaderReconcileHandle`
- `internal/koreader/service.go` — `WithReadingPositions`, `NewService`, `RegisterDevice`, `Authenticate`, `SaveProgress`, `GetProgress`, `ResetProgress`, `ReconcileProgress`, `ReconcileProgressWithMatching`, `ListAccounts`, `CreateAccount`, `RotateAccountKey`, `SetAccountEnabled`, `DeleteAccount`, `GenerateSyncKey`, `NormalizeDocumentForMatch`, `IndexedBookCount`, `Service`, `Credentials`, `ProgressPayload`, `SyncResult`, `PositionRecorder`, `Option`, `TaskHandle`, `MatchConfig`

Historical record lookup: execution.md / issue 26/27/04.

### MOVE — Persistent relocation and restart exclusivity

Implementation owner(s):
- `internal/application/relocating/service.go` — `New`, `Recover`, `Store`, `RunReader`, `Watcher`, `Effects`, `Service`
- `internal/application/relocating/runs.go` — `Preview`, `CheckRestoration`, `Confirm`, `Restore`, `ReconcilePlan`
- `internal/application/relocating/access.go` — `GuardRoot`, `relocationTopology`

Actual consumers / regression consumers:
- `internal/api/library_http.go` — `LibraryQueries`, `ComicInfoQueries`, `libraryHTTP`, `maintenanceHTTP`, `diskHTTP`, `BookRemovalStore`, `removalHTTP`, `relocationHTTP`

Historical record lookup: execution.md / issue 25/42/45.

### CFG — Configuration publication, timing and resources

Implementation owner(s):
- `internal/application/configuration/publication.go` — `Error`, `New`, `DatabasePath`, `Read`, `Status`, `SaveSettings`, `SaveExpected`, `Reload`, `Invalid`, `Publication`, `Result`, `Service`, `Snapshot`
- `internal/config/effects.go` — `FieldEffects`, `ChangedFieldEffects`, `FieldEffect`
- `internal/runtimecfg/runtimecfg.go` — `NewResources`, `Apply`, `Close`, `Resources`

Actual consumers / regression consumers:
- `internal/bootstrap/bootstrap.go` — `Open`, `Close`, `Shutdown`, `Options`, `Process`
- `internal/api/host.go` — `NewTransport`, `RuntimeEffects`, `TaskOptions`, `Bind`, `SetupRoutes`, `SetupOPDSRoutes`, `SetupKOReaderRoutes`, `Lifecycle`, `ReadingDependencies`, `RunDependencies`, `LibraryDependencies`, `AdministrationDependencies`, `HostDependencies`, `Transport`, `Host`
- `web/src/pages/settings/configVersion.test.tsx` — consumer fixture/test suite, not production owner

Historical record lookup: execution.md / issue 30/31/32; administration adapter pending46.

### V — Narrow supplier capabilities and fixed operation inputs

Implementation owner(s):
- `internal/application/intelligence/intelligence.go` — `New`, `Store`, `GroupingProvider`, `Sources`, `Service`
- `internal/application/intelligence/scrape.go` — `ScrapeAll`, `ScrapeLibrary`, `scrapeSeriesEntry`, `scrapeMetrics`
- `internal/metadata/provider.go` — `NewAIProvider`, `Fetcher`, `Searcher`, `Recommender`, `Grouper`, `ConnectionTester`, `Provider`, `AIProvider`, `AIRecommendation`

Actual consumers / regression consumers:
- `internal/api/metadata_capability_test.go` — consumer fixture/test suite, not production owner
- `internal/application/intelligence/grouping.go` — `GroupLibrary`, `Collecting`, `Requesting`, `Queueing`, `groupingObserver`

Historical record lookup: execution.md / issue 29/30; administrative transport pending46.

### RT — Production assembly, recovery before inputs, shutdown

Implementation owner(s):
- `internal/bootstrap/bootstrap.go` — `Open`, `Close`, `Shutdown`, `Options`, `Process`
- `internal/application/runtime/runtime.go` — `ExecTx`, `New`, `Recover`, `RecoverRuns`, `Go`, `Done`, `WaitBackground`, `Close`, `Config`, `Effects`, `ConfigWatch`, `Options`, `Runtime`, `proposalStore`
- `internal/application/runtime/lifecycle.go` — `SampleInterval`, `BackoffPolicy`, `Start`, `Shutdown`, `DispatchScheduledScans`

Actual consumers / regression consumers:
- `cmd/server/main.go` — module exports/types (see source)
- `internal/api/host.go` — `NewTransport`, `RuntimeEffects`, `TaskOptions`, `Bind`, `SetupRoutes`, `SetupOPDSRoutes`, `SetupKOReaderRoutes`, `Lifecycle`, `ReadingDependencies`, `RunDependencies`, `LibraryDependencies`, `AdministrationDependencies`, `HostDependencies`, `Transport`, `Host`

Historical record lookup: execution.md / issue 31/42/43/44/45; contraction pending47.

### T — Run declarations / durable history / observation contracts

Implementation owner(s):
- `internal/task/start.go` — `Start`, `coalesceLocked`, `Handle`, `ReleaseQueued`, `RunSpec`, `Result`, `Body`, `Launched`
- `internal/application/scanning/restart.go` — `Restarts`, `Restart`, `RestartDefinition`
- `internal/application/maintenance/restart.go` — `Restarts`, `Restart`, `RestartDefinition`

Actual consumers / regression consumers:
- `internal/api/task_model.go` — `taskFilters`
- `web/src/features/task-observation/useTaskObservation.ts` — `useTaskObservation`
- `web/src/components/tasks/TaskCenter.tsx` — `TaskFilters`, `RunProgressBar`, `RunControlButtons`, `RunLimitBadges`, `RunMetricsGrid`, `RunDetailDrawer`, `RunTelemetry`, `RunPhaseTimeline`, `RunFailureList`, `RunThroughputCurve`, `RunEventStream`, `RunDetailPanel`, `RunCard`, `RunCardList`, `LiveSection`, `TaskRow`, `TaskCenter`, `Translate`

Historical record lookup: execution.md / issue 36/37/45.

### OFF — Offline ownership, original facts and complete consumer lifecycle

Implementation owner(s):
- `web/src/features/offline/offlineReader.ts` — `queueOfflineProgress`, `reconcileOfflineOwner`, `syncQueuedOfflineProgress`
- `web/src/features/offline/offlineSession.ts` — `endOfflineSession`
- `web/src/features/offline/useReaderOffline.ts` — `useReaderOffline`
- `web/src/features/offline/useOfflineShelf.ts` — `useOfflineShelf`

Actual consumers / regression consumers:
- `web/src/auth/AuthProvider.tsx` — `AuthProvider`, `useAuth`, `UserRole`
- `web/src/pages/OfflineShelf.tsx` — `OfflineShelf`, `OfflineHealthBar`
- `web/src/features/offline/readerOfflineConsumer.test.tsx` — consumer fixture/test suite, not production owner

Historical record lookup: execution.md / issue 33/05/34 (browser mocks, not real-device evidence).

### EDIT — Settings partition editing and KOReader diagnostic lifecycle

Implementation owner(s):
- `web/src/features/settings-editor/EditSession.ts` — `EditSession`
- `web/src/features/koreader-settings/KOReaderProvider.tsx` — `KOReaderProvider`, `useKOReaderDiagnostics`, `useKOReaderEditing`, `EditingValue`

Actual consumers / regression consumers:
- `web/src/pages/settings/configVersion.test.tsx` — consumer fixture/test suite, not production owner
- `web/src/pages/settings/settingsLifecycle.test.tsx` — consumer fixture/test suite, not production owner

Historical record lookup: execution.md / issue 32/30; backend adapter pending46.

### FX — Queries vs committed mutation effects / scoped responses

Implementation owner(s):
- `web/src/session/ApplicationSessionProvider.tsx` — `ApplicationSessionScope`, `ApplicationSessionProvider`, `SessionInstance`, `useSession`, `useSessionQueryScope`, `useLibraryInvalidation`, `useLibraryRefresh`, `useSessionRunPush`, `useSessionRecovery`, `useSessionActive`, `LibraryArea`
- `web/src/pages/series-detail/hooks/useSeriesMetadataReview.ts` — `useSeriesMetadataReview`
- `web/src/pages/series-detail/hooks/useSeriesContext.ts` — `useSeriesContext`

Actual consumers / regression consumers:
- `web/src/session/catalogConsumers.test.tsx` — consumer fixture/test suite, not production owner
- `web/src/session/proposalConsumers.test.tsx` — consumer fixture/test suite, not production owner
- `web/src/session/ApplicationRoutes.test.tsx` — consumer fixture/test suite, not production owner

Historical record lookup: execution.md / issue 34/35/41; proposal follow-up 8e318f4.

### UI — Failure/stale/real-empty presentation and scope fencing

Implementation owner(s):
- `web/src/pages/Dashboard.tsx` — `Dashboard`, `DashboardContent`, `StatCard`, `ActivityHeatmap`, `OnboardingCard`, `StoragePieChart`
- `web/src/pages/Stats.tsx` — `Stats`, `StatsContent`, `ReviewStat`

Actual consumers / regression consumers:
- `web/src/pages/QueryFailure.test.tsx` — consumer fixture/test suite, not production owner
- `web/src/session/ApplicationSession.test.tsx` — consumer fixture/test suite, not production owner

Historical record lookup: execution.md / issue 35/41.

### ACC — Transactional site initialization, current actor, credentials

Implementation owner(s):
- `internal/accounts/initialization.go` — `NewInitialization`, `Setup`, `Authenticate`, `InitializationStore`, `SetupInput`, `Warning`, `SetupResult`, `Initialization`, `CredentialStore`, `Credentials`
- `internal/accounts/management.go` — `NewManagement`, `List`, `Create`, `Update`, `Delete`, `Actor`, `ManagementStore`, `Management`, `CreateInput`, `UpdateInput`
- `internal/accounts/password.go` — `NewPasswords`, `Change`, `Reset`, `PasswordStore`, `Passwords`, `ChangePasswordInput`, `PasswordResult`

Actual consumers / regression consumers:
- `internal/api/auth_controller.go` — `userFromContext`, `usersExist`, `authCtxKey`, `authSessionResponse`
- `web/src/auth/AuthProvider.tsx` — `AuthProvider`, `useAuth`, `UserRole`
- `web/src/pages/settings/accountManagement.test.tsx` — consumer fixture/test suite, not production owner

Historical record lookup: execution.md / issue 38/39/40; transport contraction pending46/47.

## Every-story matrix

Each row maps the original story to the catalogue’s actual source + consumer symbols and a precise regression anchor. Read status literally. Supplemental limitations are intentionally not hidden behind O+T. No row is marked current PASS.

| Story and approved intent | Source/consumer group | Precise regression evidence | Status / acceptance qualification |
|---|---|---|---|
| **US001** 作为维护者，我希望主要后端与前端功能都纳入审查，以便本轮改善覆盖整个项目而不只围绕 Controller。 | A | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` | O+T slice; P47; final structural audit M48 |
| **US002** 作为维护者，我希望审查区分文件索引、关键路径阅读和实际运行验证，以便能够判断结论的证据范围。 | A | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` | O+T slice; P47; final structural audit M48 |
| **US003** 作为维护者，我希望每个主要区域都有保留、调整或需验证的明确处置，以便未修改文件不被误认为遗漏。 | A | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` | O+T slice; P47; final structural audit M48 |
| **US004** 作为维护者，我希望业务规则、状态、事务和生命周期各有明确所有者，以便修改知识集中在所属能力。 | A | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` | O+T slice; P47; final structural audit M48; P46 |
| **US005** 作为调用者，我希望通过完整用例表达意图，以便不必自己组合校验、写入、聚合与后续动作。 | A | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` | O+T slice; P47; final structural audit M48; P46 |
| **US006** 作为维护者，我希望领域概念、查询视图、编辑输入、传输契约和持久化记录按含义区分，以便结构体归属可以从职责判断。 | A | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` | O+T slice; P47; final structural audit M48 |
| **US007** 作为维护者，我希望合理的技术辅助保留清晰职责，以便不会为采用 DDD 而增加无收益的领域包装。 | A | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` | O+T slice; P47; final structural audit M48 |
| **US008** 作为维护者，我希望模块通过所需能力建立依赖，以便不会把全站应用对象或全量 Store 变成默认入口。 | A | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` | O+T slice; P47; final structural audit M48; P46 |
| **US009** 作为维护者，我希望业务模块不反向依赖 HTTP 或页面，以便同一功能可以由不同输入和测试调用。 | A | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` | O+T slice; P47; final structural audit M48; P46 |
| **US010** 作为维护者，我希望读写各有合适的接口，以便写入一致性与查询效率都能得到保证。 | A | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` | O+T slice; P47; final structural audit M48 |
| **US011** 作为维护者，我希望使用现有单一领域语境和部署方式，以便模块整理不演变成无依据的上下文或服务拆分。 | A | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` | O+T slice; P47; final structural audit M48 — Structural retained single deployment/SQLite choice; a behavior test cannot prove this repository-wide decision. |
| **US012** 作为维护者，我希望有依据的业务差异继续保留，以便统一结构不会抹平不同来源和功能的意图。 | A | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` | O+T slice; P47; final structural audit M48 — Source-specific differences also need seriesmeta scan/manual/proposal review; do not normalize them into one policy. |
| **US013** 作为维护者，我希望每条迁移都有实际消费者且删除旧规则副本，以便不会同时维护两份权威实现。 | A | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` | O+T slice; P47; final structural audit M48; P46 — Reader contraction done; remaining HTTP forwarding bridges are explicitly pending47, not a newly discovered scope. |
| **US014** 作为维护者，我希望按规则集中和验证成本评估成果，以便文件数量、行数和层数不代替真实收益。 | A | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` | O+T slice; P47; final structural audit M48 — Navigation/knowledge review remains required; file count is not evidence. |
| **US015** 作为阅读者，我希望书的藏书身份与个人阅读状态分开表达，以便另一位读者的进度不改变这本书的身份。 | R | `internal/reading/book_view_test.go::TestBoundBookViewsKeepSubjectFactsNavigationAndUnknownCompletion` | O+T; execution U |
| **US016** 作为调用者，我希望阅读查询明确接收主体，以便结果属于谁不依赖隐含全局用户。 | R | `internal/reading/book_view_test.go::TestBookViewRequiresExplicitValidSubject` | O+T; execution U |
| **US017** 作为维护者，我希望类型能区分藏书事实与已绑定读者的阅读视图，以便调用者不会忘记先做主体投影。 | R | `internal/reading/book_view_test.go::TestBoundBookViewsKeepSubjectFactsNavigationAndUnknownCompletion` | O+T; execution U |
| **US018** 作为阅读者，我希望详情、导航和继续阅读使用同一主体的事实，以便不同入口不会展示他人或全局兼容进度。 | R | `internal/reading/book_view_test.go::TestBoundBookViewsKeepSubjectFactsNavigationAndUnknownCompletion` | O+T; execution U |
| **US019** 作为兼容客户端使用者，我希望无站点用户的路径继续保留已确认语义，以便升级不会丢失兼容阅读能力。 | R | `internal/reading/recent_view_test.go::TestRecentBooksBindsReaderAndPreservesAnonymousCompatibility` | O+T; execution U |
| **US020** 作为维护者，我希望非法主体不能被静默当作匿名，以便输入错误不会变成另一种身份的读取。 | R | `internal/reading/book_view_test.go::TestBookViewRequiresExplicitValidSubject` | O+T; execution U |
| **US021** 作为阅读者，我希望主体读态进入筛选、排序与计数后再分页，以便分页结果和总量真实反映自己的状态。 | Q | `internal/reading/catalogue_view_test.go::TestCataloguePageBindsReaderBeforeFilteringCountingOrderingAndPagination` | O+T; execution U |
| **US022** 作为维护者，我希望列表查询继续使用批量 SQL 和必要聚合，以便模型分离不会退化为逐书查询。 | Q | `internal/reading/catalogue_view_test.go::TestCataloguePageBindsReaderBeforeFilteringCountingOrderingAndPagination` | O+T; execution U — SQL/query-source review is required for batching; one correctness test does not establish query complexity. |
| **US023** 作为阅读者，我希望书、系列和跨页面读态解释一致，以便已读、未读和阅读中不会因页面而变化。 | R | `internal/reading/book_view_test.go::TestBoundBookViewsKeepSubjectFactsNavigationAndUnknownCompletion` | O+T; execution U |
| **US024** 作为阅读者，我希望未知页数的完成态与真实页数分开，以便不会把内部哨兵显示为已读页数。 | R | `internal/reading/book_view_test.go::TestBoundBookViewsKeepSubjectFactsNavigationAndUnknownCompletion` | O+T; execution U |
| **US025** 作为阅读者，我希望有效新位置继续允许前进和回退，以便结构调整不恢复旧的最远页策略。 | W | `internal/reading/position_test.go::TestPositionAllowsNewerBackwardReadingAndRejectsDelayedReports` | O+T; execution U |
| **US026** 作为离线阅读者，我希望原时间、事实身份及清除后的拒旧规则继续有效，以便视图整理不破坏上一轮已交付的写入语义。 | W | `internal/reading/mark_test.go::TestClearEvidenceSurvivesRestartForUserAndAnonymousReader` | O+T; execution U |
| **US027** 作为阅读者，我希望普通继续阅读与有序清单的重读回退各保留用途，以便全部已读后仍可从清单首书重读。 | R | `internal/organize/reader_queries_test.go::TestOrganizedReaderQueriesBindSubjectsAndKeepListFallbackSeparate` | O+T; execution U |
| **US028** 作为客户端使用者，我希望既有 JSON 和 TypeScript 字段保持兼容，以便内部模型变化不迫使客户端同步改版。 | R | `internal/reading/book_view_test.go::TestBoundBookViewsKeepSubjectFactsNavigationAndUnknownCompletion` | O+T; execution U — Existing public consumer anchors; fresh tsgen drift check is still unexecuted. |
| **US029** 作为维护者，我希望缓存区分主体及所依赖事实，以便不能把一个人的视图复用给另一个人。 | Q | `internal/bootstrap/bootstrap_test.go::TestHTTPQueryCacheReceivesTheApplicationInvalidation` | O+T; execution U — Also readerCatalogueCache.test.tsx: does not reuse another reader response for the same catalogue query. |
| **US030** 作为维护者，我希望基本阅读规则可经完整查询和用例验证，以便无需构造整个 HTTP Controller。 | R | `internal/reading/queries_test.go::TestReaderViewsIsolateSubjectAndReflectBackwardPositionAndClear` | O+T; execution U |
| **US031** 作为维护者，我希望事务能力只提供完整一致性操作所需内容，以便窄接口不能通过回调穿透为全部 SQL。 | W | `internal/reading/atomic_store_test.go::TestReadingWritesUseOnlyAtomicStorageCapabilities` | O+T; execution U |
| **US032** 作为维护者，我希望事务归属按业务不变量确定，以便不会按一张表或一个 handler 机械划界。 | W | `internal/reading/mark_test.go::TestMarkBatchFailureRollsBackProgressBasisAndAggregate` | O+T; execution U |
| **US033** 作为调用者，我希望人工保存元数据是完整动作，以便不必补上事务、系列统计和响应重读配方。 | M | `internal/application/seriesediting/manual_test.go::TestManualSaveReturnsCommittedMetadataAndPreservesAbsentCollections` | O+T; execution U |
| **US034** 作为管理员，我希望人工编辑的未提供字段与显式清空继续区分，以便既有编辑意图不被改变。 | M | `internal/application/seriesediting/manual_test.go::TestManualSaveReturnsCommittedMetadataAndPreservesAbsentCollections` | O+T; execution U |
| **US035** 作为管理员，我希望批量编辑继续保留标量、标签添加与移除的既有次序和语义，以便抽取不会改变结果。 | M | `internal/seriesmeta/bulk_test.go::TestSelectionPreservesRawScalarsAndRemovesAfterAdding` | O+T; execution U |
| **US036** 作为管理员，我希望扫描写入继续遵守标题和元数据锁定，以便摄入不会覆盖人工保护。 | S | `internal/scanner/series_title_lock_test.go::TestScanKeepsUserEditedSeriesTitleAndLocks` | O+T; execution U — Also seriesmeta TestScanInitialMetadataAndExistingLocks and TestScanTitleFallbackAndLockedScalarMatrix. Scan association append policy intentionally differs from manual/proposal replacement. |
| **US037** 作为管理员，我希望提案应用继续遵守来源规则、锁定与字段选择，以便不会被统一成无差别覆盖。 | P | `internal/proposal/resolve_test.go::TestApplyFiltersFieldsLockedAfterQueueing` | O+T; execution U |
| **US038** 作为维护者，我希望元数据来源规则仍有单一归属，以便完整应用用例不会复制另一份规则。 | P | `internal/proposal/resolve_test.go::TestApplyFiltersFieldsLockedAfterQueueing` | O+T; execution U |
| **US039** 作为维护者，我希望存储适配避免形成包依赖循环，以便业务类型分离后仍可编译和独立验证。 | M | `internal/application/seriesediting/manual_test.go::TestManualSaveRollsBackRequiredStatisticsAndConflictingVersion` | O+T; execution U |
| **US040** 作为维护者，我希望只隔离有业务含义的存储形状，以便纯查询不产生大量无规则收益的转换。 | M | `internal/reading/catalogue_view_test.go::TestCataloguePageBindsReaderBeforeFilteringCountingOrderingAndPagination` | O+T; execution U — This is a deliberate mapping-restraint/architecture decision, not a demand for more DTO wrappers. |
| **US041** 作为操作发起者，我希望结果区分未提交、部分提交和已提交但后续未完成，以便可以安全判断是否重试。 | W | `internal/reading/mark_test.go::TestLaterMarkBatchFailureRetainsAndInvalidatesCommittedRange` | O+T; execution U |
| **US042** 作为操作发起者，我希望前批已提交结果在后批失败后继续有效，以便错误报告不会掩盖已发生事实。 | W | `internal/reading/mark_test.go::TestLaterMarkBatchFailureRetainsAndInvalidatesCommittedRange` | O+T; execution U |
| **US043** 作为维护者，我希望进度已保存而统计失败仍独立告警，以便不通过强事务回滚改变已批准的软统计语义。 | W | `internal/reading/position_test.go::TestActivityFailureKeepsCommittedPositionAndAttemptsUserProjection` | O+T; execution U |
| **US044** 作为维护者，我希望退出无人使用且可绕过新规则的旧写入口，以便未来修改不会重新长出第二条写路径。 | W | `internal/reading/atomic_store_test.go::TestReadingWritesUseOnlyAtomicStorageCapabilities` | O+T slice; P47; final structural audit M48 — Old reading write APIs removed, but whole-host legacy bridges remain pending47. |
| **US045** 作为审核者，我希望完整应用和部分应用都在写入事务中确认提案仍待裁决，以便终态之后不再写入字段。 | P | `internal/proposal/concurrency_test.go::TestPartialApplyConflictsWhenRejectCommitsBeforeItsTransaction` | O+T; execution U |
| **US046** 作为审核者，我希望并发拒绝先完成时后来的部分应用返回冲突，以便抢先完成的裁决得到尊重。 | P | `internal/proposal/concurrency_test.go::TestPartialApplyConflictsWhenRejectCommitsBeforeItsTransaction` | O+T; execution U |
| **US047** 作为审核者，我希望冲突时本次元数据、提案字段和来源沿革都不提交，以便审计与实际结果一致。 | P | `internal/proposal/concurrency_test.go::TestPartialApplyFailureRollsBackMetadataFieldsAndProvenance` | O+T; execution U |
| **US048** 作为审核者，我希望拒绝不撤销此前成功应用的字段，以便已确认事实不会被后续拒绝反向抹去。 | P | `internal/proposal/concurrency_test.go::TestConcurrentPartialApplicationsDoNotReuseConsumedFields` | O+T; execution U — Concurrency suite includes rejection preserving fields committed by an earlier partial apply; not rollback of all history. |
| **US049** 作为审核者，我希望部分应用成功后保留剩余待裁决字段，以便可以继续处理未应用部分。 | P | `internal/proposal/resolve_test.go::TestApplyFillEmptyKeepsUnappliedFields` | O+T; execution U |
| **US050** 作为审核者，我希望应用依据事务内最新字段、锁定和系列事实选择，以便不使用加载后已过期的选择结果。 | P | `internal/proposal/concurrency_test.go::TestApplySelectsUsingTransactionCurrentValuesAndLocks` | O+T; execution U |
| **US051** 作为审核者，我希望两个部分应用交错时不会重复使用已移除字段，以便来源和剩余建议保持一致。 | P | `internal/proposal/concurrency_test.go::TestConcurrentPartialApplicationsDoNotReuseConsumedFields` | O+T; execution U |
| **US052** 作为审核者，我希望完整应用仍能正确进入终态且不可重复裁决，以便原有状态语义保持有效。 | P | `internal/proposal/resolve_test.go::TestApplyWritesEveryFieldAndClosesProposal` | O+T; execution U |
| **US053** 作为审核者，我希望全部锁定或没有可写变化仍得到准确结果，以便不会把未写入误报为应用完成。 | P | `internal/proposal/resolve_test.go::TestApplyReportsAllFieldsLockedWhenEverythingIsFiltered` | O+T; execution U |
| **US054** 作为客户端使用者，我希望裁决冲突与系统故障可区分，以便页面可以重读当前状态而不是误报保存失败。 | P | `internal/api/metadata_review_concurrency_test.go::TestPartialApplyHTTPClassifiesTransactionCurrentOutcome`; `web/src/pages/metadataReviewConcurrency.test.tsx` :: shows conflicts, no changes and faults separately after a partial-apply request | O+T; execution U |
| **US055** 作为维护者，我希望真实并发事务覆盖裁决竞争，以便事务外预检不能被当作唯一正确性保证。 | P | `internal/proposal/concurrency_test.go::TestConcurrentPartialApplicationsDoNotReuseConsumedFields` | O+T; execution U — Real SQLite transaction barrier tests, not merely preflight/mock checks. |
| **US056** 作为管理员，我希望人工合集、智能书架、快照、作品群和阅读清单保持不同成员语义，以便结构统一不改变编辑权限。 | C | `internal/organize/collections_test.go::TestOnlyContinuouslyDerivedCollectionsRejectManualChanges` | O+T; execution U |
| **US057** 作为管理员，我希望一次性快照和 AI 落地合集继续归人工所有，以便来源不会被误认为持续推导。 | C | `internal/organize/smart_collections_test.go::TestSmartSnapshotIsOneTimeAndManuallyOwned` | O+T; execution U |
| **US058** 作为管理员，我希望合集名称、归属和成员修改通过完整用例维护，以便输入不需要了解跨表约束。 | C | `internal/organize/collections_test.go::TestMemberBatchIsAtomicForMissingSeries` | O+T; execution U |
| **US059** 作为阅读者，我希望作品群身份和成员差集更新保持稳定，以便重建不使收藏入口或链接无故变化。 | F | `internal/organize/relations_test.go::TestFranchiseMergeSplitKeepsUnaffectedIdentitiesAndTimes` | O+T; execution U |
| **US060** 作为管理员，我希望关系保存可以先成功而作品群随后更新，以便每次编辑不等待整个关系图重建事务。 | F | `internal/organize/franchise_repair_test.go::TestRelationWriteRollsBackWhenDurableMarkFails` | O+T; execution U |
| **US061** 作为维护者，我希望关系变化与待更新依据同事务提交，以便关系已变但修复责任不会丢失。 | F | `internal/organize/franchise_repair_test.go::TestRelationWriteRollsBackWhenDurableMarkFails` | O+T; execution U |
| **US062** 作为维护者，我希望派生更新依据覆盖全局关系图，以便跨资料库关系不会被错误分割。 | F | `internal/organize/franchise_repair_test.go::TestRelationWriteRollsBackWhenDurableMarkFails` | O+T; execution U — Global graph coverage must be inspected in franchise relations across libraries; referenced rollback test alone is not complete proof. |
| **US063** 作为维护者，我希望系列和资料库删除导致的级联变化也标记派生结果待更新，以便修复不会只覆盖显式关系编辑。 | F | `internal/organize/franchise_facts_test.go::TestFranchiseCascadeSourcesRecoverAfterReopenWithStableIdentity` | O+T; execution U |
| **US064** 作为维护者，我希望影响派生结果的系列事实变化也进入更新依据，以便成员名称等展示不会永久陈旧。 | F | `internal/organize/franchise_facts_test.go::TestManualTitleEditPersistsFranchiseFactChange` | O+T; execution U |
| **US065** 作为阅读者，我希望重建失败后系统退避重试，以便一次暂时错误不会永久留下旧结果。 | F | `internal/organize/franchise_repair_test.go::TestFranchiseFailureKeepsOldProjectionAndHonorsBackoffAcrossReopen` | O+T; execution U |
| **US066** 作为阅读者，我希望服务重启后仍继续未完成重建，以便内存通知丢失不影响最终修复。 | F | `internal/application/runtime/franchise_repair_test.go::TestRuntimeResumesDurableFranchiseRepairAndRetriesFailure` | O+T; execution U |
| **US067** 作为维护者，我希望重建期间的新变化不会被旧成功清除，以便系统不会误以为已追上最新关系。 | F | `internal/organize/franchise_repair_test.go::TestNewRelationChangeCannotBeClearedByOldRebuildCompletion` | O+T; execution U |
| **US068** 作为维护者，我希望旧尝试的失败不能覆盖较新的成功状态，以便修复状态反映实际版本。 | F | `internal/organize/franchise_repair_test.go::TestOldFranchiseFailureCannotReplaceNewerSuccess` | O+T; execution U |
| **US069** 作为阅读者，我希望已有成功作品群结果在失败期间仍可读且标明陈旧，以便保持可用性而不误信其已更新。 | F | `internal/organize/franchise_repair_test.go::TestFranchiseUpgradePreservesExistingResultAsStale`; `web/src/features/organize/FranchiseRepairStatus.test.tsx` :: keeps old results identified as stale and retries immediately after a failure | O+T; execution U |
| **US070** 作为阅读者，我希望首次没有成功结果时显示不可用而不是空结果，以便不会误判没有作品群。 | F | `internal/organize/franchise_repair_test.go::TestFranchiseRepairResponsibilitySurvivesWithoutBackgroundNotification`; `web/src/features/organize/FranchiseRepairStatus.test.tsx` :: distinguishes a first unavailable projection from a successful empty projection | O+T; execution U |
| **US071** 作为管理员，我希望可以立即重试派生更新且后台责任持续保留，以便可以主动恢复而不依赖下一次关系修改。 | F | `internal/organize/franchise_repair_test.go::TestFranchiseRepairResponsibilitySurvivesWithoutBackgroundNotification`; `web/src/features/organize/FranchiseRepairStatus.test.tsx` :: keeps old results identified as stale and retries immediately after a failure | O+T; execution U — Immediate retry has frontend consumer coverage; pair it with persisted RepairDue responsibility. |
| **US072** 作为管理员，我希望创建、修改和删除资料库各有完整用例，以便 HTTP 与导航壳不再持有操作配方。 | L | `internal/application/libraries/save_test.go::TestCreateLibraryKeepsCommittedResultWhenInitialScanAdmissionFails` | O+T; execution U |
| **US073** 作为管理员，我希望资料库路径、格式、间隔和已有路径占用有一致校验，以便不同输入不会绕开约束。 | L | `internal/application/libraries/save_test.go::TestLibraryValidationRejectsEquivalentRegisteredRootAcrossCreateAndUpdate` | O+T; execution U |
| **US074** 作为管理员，我希望新建后的监听、首扫和刷新得到完整交接，以便资料库创建成功不被等同为扫描完成。 | L | `internal/application/libraries/save_test.go::TestCreateLibraryKeepsCommittedResultWhenInitialScanAdmissionFails` | O+T; execution U |
| **US075** 作为管理员，我希望有记录的资料库改路径继续使用保留身份的迁移流程，以便普通设置修改不会破坏记录。 | L | `internal/application/libraries/save_test.go::TestUpdateLibraryPreservesIdentityAndRejectsOccupiedPathChange` | O+T; execution U |
| **US076** 作为管理员，我希望保护恢复约束继续限制自动处理和危险清理，以便迁移后异常记录不会被误删。 | L | `internal/application/scanning/scanning_test.go::TestProtectedLibraryRejectsAutomaticScansAndEveryCleanup` | O+T; execution U |
| **US077** 作为管理员，我希望删库先关闭该库新工作和读取准入，以便等待期间不会不断增加占用。 | L | `internal/task/library_reservation_test.go::TestLibraryReservationBlocksQueueAndReleasesOtherLibraries` | O+T; execution U |
| **US078** 作为管理员，我希望已排队工作在删库保留期间也不能开跑，以便关闭准入不是只拦住新发起。 | L | `internal/task/library_reservation_test.go::TestLibraryReservationBlocksQueueAndReleasesOtherLibraries` | O+T; execution U |
| **US079** 作为管理员，我希望可取消的本库运行被请求取消并有界等待，以便删除不会无限挂起。 | L | `internal/application/libraries/delete_test.go::TestDeleteCancelsLocalRunAndWaitsForItsActualExit` | O+T; execution U |
| **US080** 作为管理员，我希望仍有阅读、不可取消或跨库工作占用时明确阻止删除，以便不会靠外键失败收拾晚到写入。 | L | `internal/application/libraries/delete_test.go::TestDeleteBusyLibraryPreservesRecordAndWatcherThenDeletesWhenReadExits` | O+T; execution U |
| **US081** 作为管理员，我希望删一个库不自动取消覆盖其他库的系统运行，以便其他资料库工作不受牵连。 | L | `internal/task/library_drain_test.go::TestLibraryDrainCancelsOnlyEligibleIdentityAndActualCoverage` | O+T; execution U |
| **US082** 作为维护者，我希望取消资格结合实际覆盖和任务身份判断，以便任务键前缀或单个库标识不造成误判。 | L | `internal/task/library_drain_test.go::TestLibraryDrainCancelsOnlyEligibleIdentityAndActualCoverage` | O+T; execution U |
| **US083** 作为维护者，我希望等待的是访问者真实退出而不只是展示终态，以便资源仍在使用时不会删记录。 | L | `internal/task/library_reservation_test.go::TestLibraryReservationWaitsForBodyExitDespiteTerminalSnapshot` | O+T; execution U |
| **US084** 作为维护者，我希望占用排空与升级独占之间没有重新准入窗口，以便并发访问不能穿过删除边界。 | L | `internal/task/library_reservation_test.go::TestLibraryReservationUpgradeNeverReopensReadAdmission` | O+T; execution U |
| **US085** 作为管理员，我希望请求取消、超时或删除失败后准入被释放，以便仍存在的资料库可继续使用。 | L | `internal/application/libraries/delete_test.go::TestDeleteHoldsEngineExclusivityUntilCommitAndCancellationReleasesIt` | O+T; execution U |
| **US086** 作为管理员，我希望删除失败后按当前设置恢复应有监听，以便资料库不会悄悄失去自动发现能力。 | L | `internal/application/libraries/delete_test.go::TestDeleteFailureUsesCurrentWatchSettings` | O+T; execution U |
| **US087** 作为管理员，我希望失败结果说明已取消的运行不会自动复活，以便可以有意识地重新发起工作。 | L | `internal/application/libraries/delete_test.go::TestDeleteFailureRetainsQueuedCancellationFactsWithoutRestart` | O+T; execution U |
| **US088** 作为维护者，我希望删除释放后适用队列恢复放行且其他库不被阻塞，以便协调能力不会造成永久停滞。 | L | `internal/task/library_reservation_test.go::TestLibraryReservationBlocksQueueAndReleasesOtherLibraries` | O+T; execution U |
| **US089** 作为管理员，我希望删除成功后统计、阅读路径、推荐和派生结果得到必要失效或修复，以便不继续看到已删资料库的旧内容。 | L | `internal/organize/franchise_facts_test.go::TestFranchiseSeriesDeletionPersistsRepairResponsibility` | O+T; execution U — Cascade repair pointer is not proof of every post-delete cache consumer; verify B/L/FX invalidation handoff. |
| **US090** 作为阅读者，我希望页清单、页读取和整书打开使用一致内容来源，以便缓存和访问资格不因入口不同而分叉。 | B | `internal/bookcontent/page_test.go::TestPageMaterializesBytesAndReleasesBorrowBeforeReturning` | O+T; execution U |
| **US091** 作为调用者，我希望内容能力隐藏来源定位、归档借还、加工和缓存资格，以便不必重新拼接读取配方。 | B | `internal/bookcontent/page_test.go::TestPageMaterializesBytesAndReleasesBorrowBeforeReturning` | O+T; execution U |
| **US092** 作为维护者，我希望内容事实与 HTTP 响应映射职责分开，以便技术读取能力不依赖请求对象或响应状态码。 | B | `internal/bookcontent/page_test.go::TestPageMaterializesBytesAndReleasesBorrowBeforeReturning` | O+T; execution U |
| **US093** 作为阅读者，我希望整书下载在句柄关闭前保留所需访问资格，以便删除、迁移或回写不会强行截断已开始的读取。 | B | `internal/bookcontent/file_test.go::TestWholeBookHandleRetainsAccessUntilClose` | O+T; execution U |
| **US094** 作为维护者，我希望已物化页字节及时释放归档借用，以便资源占用符合实际使用范围。 | B | `internal/bookcontent/page_test.go::TestPageMaterializesBytesAndReleasesBorrowBeforeReturning` | O+T; execution U |
| **US095** 作为阅读者，我希望图像透传、真实加工结果和缓存资格继续区分，以便没有实际加工的结果不会被错误缓存。 | B | `internal/bookcontent/page_test.go::TestPassthroughAndUnavailableFilterStayOutOfProcessedCache` | O+T; execution U |
| **US096** 作为阅读者，我希望图像尺寸、解压与内存保护继续有效，以便内容抽取不会扩大危险输入的资源影响。 | B | `internal/images/processor_process_test.go::TestProcessImageRejectsDecodeBomb` | O+T; execution U — Also parser TestReadEntryLimited and images TestProcessImageRejectsUnsafeTargetDimensions; actual image hardware/AI processing execution remains unexecuted. |
| **US097** 作为阅读者，我希望缓存包含影响字节的参数与来源依据，以便变更图像选项或替换归档后不会读取旧内容。 | B | `internal/bookcontent/page_test.go::TestDerivedPageCacheTracksVariantAndArchivePath` | O+T; execution U |
| **US098** 作为管理员，我希望 ComicInfo 临时归档构建尽量不阻塞阅读，以便耗时处理不占据整段写入窗口。 | PUB | `internal/parser/publication_test.go::TestComicInfoPublicationWaitsForEvictedBorrowAndBlocksNewReaders` | O+T; execution U |
| **US099** 作为管理员，我希望最终替换前取得该书短期写入资格并阻止新借用，以便在稳定发布窗口处理原归档。 | PUB | `internal/parser/publication_test.go::TestComicInfoPublicationWaitsForEvictedBorrowAndBlocksNewReaders` | O+T; execution U |
| **US100** 作为管理员，我希望发布有界等待已有读取结束，以便工作不会无限等待且不强制切断读者。 | PUB | `internal/parser/publication_test.go::TestComicInfoPublicationTimeoutCancelAndSourceChangePreserveOriginal` | O+T; execution U |
| **US101** 作为管理员，我希望原文件已变化或读取仍占用时明确失败并保留原文件，以便不覆盖外部修改或正在使用的内容。 | PUB | `internal/parser/publication_test.go::TestComicInfoPublicationTimeoutCancelAndSourceChangePreserveOriginal` | O+T; execution U |
| **US102** 作为维护者，我希望归档池淘汰与最后借用者退出被分别处理，以便移出缓存不会被误认为文件已经关闭。 | PUB | `internal/parser/publication_test.go::TestComicInfoPublicationWaitsForEvictedBorrowAndBlocksNewReaders` | O+T; execution U |
| **US103** 作为管理员，我希望成功发布后内容事实和缓存得到统一交接，以便新请求不继续采用旧来源。 | PUB | `internal/bookcontent/publication_test.go::TestPublicationRetainsCommittedFactWhenFactStoreFails` | O+T; execution U — Publication invalidates cache even on fact-store warning. Actual next-request-new-bytes consumer should be checked through bound Host. |
| **US104** 作为管理员，我希望发布后的后续失败保留文件已提交事实，以便不会因模糊失败无条件再次改写文件。 | PUB | `internal/bookcontent/publication_test.go::TestPublicationRetainsCommittedFactWhenFactStoreFails` | O+T; execution U |
| **US105** 作为维护者，我希望真实平台文件系统验证替换和失败保留，以便不只用模拟 Rename 成功证明安全。 | PUB | `internal/parser/publication_test.go::TestComicInfoPublicationTimeoutCancelAndSourceChangePreserveOriginal` | O+T; execution U; platform/device/provider U — Real temp-file tests are present; Windows historical execution only, Linux/macOS runtime unexecuted here. |
| **US106** 作为管理员，我希望外部路径不与任何已登记资料库重叠，以便其他资料库不会被隐式当作外部库。 | X | `internal/external/boundary_test.go::TestExternalRootRejectsEveryRegisteredLibrary` | O+T; execution U |
| **US107** 作为管理员，我希望外部路径包含、位于或等于任一资料库目录时都被拒绝，以便祖先目录也不能绕过边界。 | X | `internal/external/boundary_test.go::TestExternalRootRejectsEveryRegisteredLibrary` | O+T; execution U |
| **US108** 作为维护者，我希望扫描、传输和保留计划遵守同一资料库访问协调，以便路径改变后旧计划不会继续读写。 | X | `internal/application/diskjobs/diskjobs_test.go::TestQueuedExportRetainsCoverageAndCancellationNeverCopies` | O+T; execution U — Same Engine coverage and stale queued plan tested; path-alias checks are platform dependent. |
| **US109** 作为管理员，我希望传输仍使用明确准备结果和实际运行，以便准入失败、无缺书和运行完成不是同一种结果。 | X | `internal/application/diskjobs/diskjobs_test.go::TestTransferExternalCopiesAndRefreshesSession` | O+T; execution U |
| **US110** 作为管理员，我希望取消可以在当前文件复制块之间生效并停止后续复制，以便不必等整本大文件传完。 | X | `internal/external/transfer_test.go::TestTransferCancellationKeepsCommittedBooksAndRemovesCurrentStaging` | O+T; execution U |
| **US111** 作为管理员，我希望取消清理本次未提交临时文件，以便不会留下可被误当成完整书的目标。 | X | `internal/external/transfer_test.go::TestTransferCancellationKeepsCommittedBooksAndRemovesCurrentStaging` | O+T; execution U |
| **US112** 作为管理员，我希望取消保留之前成功传输的文件，以便不会丢失已经完成的成果。 | X | `internal/external/transfer_test.go::TestTransferCancellationKeepsCommittedBooksAndRemovesCurrentStaging` | O+T; execution U |
| **US113** 作为管理员，我希望已有目标文件始终保留，以便复制竞争或重试不覆盖他人已经放好的文件。 | X | `internal/external/transfer_test.go::TestTransferPreservesDestinationCreatedDuringCopy` | O+T; execution U |
| **US114** 作为管理员，我希望暂停在复制块边界生效并保留临时文件及偏移，以便恢复时继续当前书。 | X | `internal/external/transfer_pause_test.go::TestTransferPausesWithinCurrentBookAndResumesSameStaging` | O+T; execution U |
| **US115** 作为阅读者，我希望暂停传输释放可重新取得的 IO 配额，以便暂停作业不会长期占用阅读所需资源。 | X | `internal/external/transfer_pause_test.go::TestTransferPausesWithinCurrentBookAndResumesSameStaging` | O+T; execution U — Quota release is asserted by pause session tests; not a promise to interrupt an in-flight OS write. |
| **US116** 作为管理员，我希望恢复前核对源文件和目标资格，以便不能确认安全时不拼接不一致内容。 | X | `internal/external/transfer_pause_test.go::TestPausedTransferRejectsChangedSourceOrStagingAndCleansUp` | O+T; execution U |
| **US117** 作为管理员，我希望不能安全续拷时明确失败并清理本次临时文件，以便原有文件与已完成成果仍受保护。 | X | `internal/external/transfer_pause_test.go::TestPausedTransferRejectsChangedSourceOrStagingAndCleansUp` | O+T; execution U |
| **US118** 作为管理员，我希望未提交文件不计为成功，以便暂停、取消和进度展示反映真实完成量。 | X | `internal/external/transfer_test.go::TestTransferCancellationKeepsCommittedBooksAndRemovesCurrentStaging` | O+T; execution U |
| **US119** 作为管理员，我希望传输部分失败时保留已完成范围并刷新会话投影，以便页面不丢失已发生的成果。 | X | `internal/external/transfer_test.go::TestTransferFailureKeepsSuccessfulRangeAndSessionProjection` | O+T; execution U — Owner/session result and frontend live/reconnect tests exist; combined publication->later failure->reconnect coverage remains an acceptance check. |
| **US120** 作为维护者，我希望临时复制、资源归还和最终提交由完整能力处理，以便调用方不依赖内部 CopyFile 步骤。 | X | `internal/external/transfer_test.go::TestTransferCancellationKeepsCommittedBooksAndRemovesCurrentStaging` | O+T; execution U |
| **US121** 作为管理员，我希望运行取消和暂停不承诺强行中断系统内阻塞的单次读写，以便界面与实际能力一致。 | X | `internal/external/transfer_pause_test.go::TestTransferPausedWhileQueuedForQuotaDoesNotStartRead` | O+T; execution U — Explicit non-guarantee: no forced interruption of one OS-blocked read/write. Test proves cooperative boundary only. |
| **US122** 作为管理员，我希望本轮保持外部传输不跨进程自动续传，以便重启不会自动恢复可能有写盘后果的操作。 | X | `internal/application/diskjobs/diskjobs_test.go::TestQueuedExportRetainsCoverageAndCancellationNeverCopies` | O+T; execution U — Negative product boundary: no automatic cross-process transfer resume. Registry/restart inventory/manual check needed, not a resumable-transfer implementation. |
| **US123** 作为维护者，我希望扫描内部按发现、解释、身份认领、入库与清理组织知识，以便复杂性不只从 Controller 移到另一个大实现。 | S | `internal/scanner/ingestion_transaction_test.go::TestScanDoesNotReportRolledBackRenameAsCommitted` | O+T; execution U |
| **US124** 作为管理员，我希望扫描仍是完整摄入用例，以便输入不暴露 worker 与 SQL 刷写配方。 | S | `internal/scanner/ingestion_transaction_test.go::TestScanDoesNotReportRolledBackRenameAsCommitted` | O+T; execution U |
| **US125** 作为维护者，我希望技术监听交付文件变化，摄入协调负责扫描和清理资格，以便 fsnotify 不承担完整业务流程。 | S | `internal/application/scanning/watch_queue_test.go::TestQueuedWatchCleanupRechecksInputAndChangeQualification` | O+T; execution U |
| **US126** 作为阅读者，我希望改名后的记录重连完成后才可能清理，以便阅读历史不因过早清理丢失。 | S | `internal/application/scanning/scanning_test.go::TestWatchRenameCompletesBeforeCleanup` | O+T; execution U |
| **US127** 作为维护者，我希望扫描合并、排队、停发与真实成功继续区分，以便清理不把准入结果当作扫描完成。 | S | `internal/application/scanning/watch_queue_test.go::TestQueuedWatchCleanupRechecksInputAndChangeQualification` | O+T; execution U |
| **US128** 作为管理员，我希望失败扫描后的重新排期和安全清理保护保留，以便暂时故障不触发危险删除。 | S | `internal/scanner/watcher_rename_order_test.go::TestCleanupSkippedWhenSameLibraryScanFails` | O+T; execution U |
| **US129** 作为维护者，我希望扫描与封面交接保留独立运行和唯一准入，以便内部整理不制造第二套锁或隐式生成路径。 | S | `internal/application/scanning/cover_test.go::TestApplicationScanCompletesWhileItsCoverRunStillWaits` | O+T; execution U |
| **US130** 作为维护者，我希望书身份、内容证据和设备文档键分别表达，以便它们不会因同名指纹被当作同一身份。 | I | `internal/bookidentity/evidence_test.go::TestQuickAndPathEvidenceKeepCompatibleByteConventions` | O+T; execution U — Evidence helper tests and scanner/Koreader consumer review required; rename test alone is not full device-key compatibility. |
| **US131** 作为维护者，我希望扫描和维护获取身份能力时不依赖整个 KOReader 协议模块，以便依赖方向符合实际用途。 | I | `internal/bookidentity/rebuild_test.go::TestRebuildUsesEvidenceWithoutChangingBookIdentityAndSkipsUnreadableTail` | O+T; execution U |
| **US132** 作为设备用户，我希望认证、账号、匹配与协议记录继续由设备协议能力处理，以便身份工具归位不改变同步协议。 | I | `internal/api/controller_test.go::TestKOReaderSelfRegistrationCreatesAuthenticatableAccount` | O+T; execution U |
| **US133** 作为阅读者，我希望现有哈希和路径匹配口径保持兼容，以便升级不会重新认错书或丢失关联。 | I | `internal/koreader/fingerprint_extra_test.go::TestNormalizeDocumentForMatchModes` | O+T; execution U; platform/device/provider U — Hash/path matching algorithm intentionally unchanged; actual device synchronization is unexecuted. |
| **US134** 作为管理员，我希望 size+mtime 改名重连取舍和歧义保护继续有效，以便模块调整不静默改变身份算法。 | I | `internal/scanner/rehome_test.go::TestRenameIndexRefusesAmbiguousMatch` | O+T; execution U |
| **US135** 作为管理员，我希望清理的根可达性、真实书与删除比例保护继续有效，以便整理不取消已有数据保护。 | S | `internal/scanner/cleanup_guard_test.go::TestCleanupCircuitBreakerStopsMassDeletion` | O+T; execution U |
| **US136** 作为管理员，我希望资料库迁移的持久过程、决策和落盘职责明确且保留身份、保护、配置补偿与恢复独占，以便分工变化不破坏恢复能力。 | MOVE | `internal/application/relocating/relocating_test.go::TestRuntimePolicyFailureKeepsAccessProtectedUntilRecovery` | O+T; execution U — Pair owner test with API TestRelocationCommittedEffectsFailureBlocksUntilRealStartupRecovery and restoration tests. |
| **US137** 作为维护者，我希望配置发布由明确能力协调 API 保存与文件热重载，以便两条生效路径不会分叉。 | CFG | `internal/api/config_consistency_test.go::TestConfigUpdatesSerializeSaveAndReload` | O+T; execution U; transport P46 — Publication owner already exists; transport decoupling pending46. Do not treat isolated worktree changes as integrated. |
| **US138** 作为管理员，我希望设置区分即时、后续作业和重启后生效，以便保存成功不会被误认为所有行为已经变化。 | CFG | `internal/config/effects_test.go::TestFieldTimingCatalogCoversConfigurationAndKeepsStartupFieldsExplicit` | O+T; execution U |
| **US139** 作为管理员，我希望一次操作的目标、来源和写入意图不因热更新半途改变，以便操作结果仍符合发起时的意图。 | CFG | `internal/application/intelligence/configuration_snapshot_test.go::TestGroupingPinsSourceBeforeDeferredRunStarts` | O+T; execution U — Fixed operation snapshots exist in intelligence/maintenance configuration_snapshot_test.go; select exact tests in appendix. |
| **US140** 作为维护者，我希望资源预算在安全切换点更新，以便旧使用者能正常归还取得的资源。 | CFG | `internal/runtimecfg/runtimecfg_test.go::TestOwnedResourcesKeepBorrowUsableAcrossUpdateAndRetirement` | O+T; execution U |
| **US141** 作为阅读者，我希望正在使用的池和处理器不会被强制销毁，以便热更新不打断活跃读取。 | CFG | `internal/runtimecfg/runtimecfg_test.go::TestOwnedResourcesKeepBorrowUsableAcrossUpdateAndRetirement` | O+T; execution U |
| **US142** 作为维护者，我希望持久化、重新加载和跨参与者协调保留不同补偿语义，以便不能用通用配置实体掩盖文件与数据库边界。 | CFG | `internal/runtimecfg/runtimecfg_test.go::TestRejectedConfigurationPreservesPublishedResources` | O+T; execution U — Save/Reload/Coordinate compensation are intentionally different; no universal transaction/outbox required. |
| **US143** 作为管理员，我希望配置版本冲突、密钥占位和校验语义继续有效，以便结构整理不覆盖他人设置或泄露凭据。 | CFG | `internal/config/config_extra_test.go::TestMaskAndRestoreAllSecrets` | O+T; execution U |
| **US144** 作为维护者，我希望进程资源有明确创建、替换与关闭所有者，以便全局状态不会成为隐含依赖。 | CFG | `internal/application/runtime/resource_lifetime_test.go::TestShutdownRetiresResourcesAfterAdmittedReaderExits` | O+T; execution U — Process singletons retained intentionally; ownership is not a multi-instance rewrite. |
| **US145** 作为维护者，我希望允许保留有依据的进程单实例资源，以便不会为形式一致增加无收益的多实例机制。 | CFG | `internal/application/runtime/resource_lifetime_test.go::TestShutdownRetiresResourcesAfterAdmittedReaderExits` | O+T; execution U |
| **US146** 作为调用者，我希望元数据搜索、推荐、分组和连接测试只依赖所需能力，以便不被迫认识供应商超集接口。 | V | `internal/api/metadata_capability_test.go::TestMetadataSearchConsumesSearchOnlySource` | O+T; execution U; transport P46 — Consumer port implemented; final admin endpoint wiring pending46. |
| **US147** 作为维护者，我希望同一供应商可以实现多个真实消费接口，以便不为接口拆分增加无行为转发层。 | V | `internal/api/metadata_capability_test.go::TestMetadataSearchConsumesSearchOnlySource` | O+T; execution U |
| **US148** 作为维护者，我希望上游 DTO、分页、重试和错误归一留在适配实现，以便供应商细节不进入业务模型。 | V | `internal/api/metadata_capability_test.go::TestMetadataSearchConsumesSearchOnlySource` | O+T; execution U — Supplier DTO/retry/cancel normalization needs source review; search-only mock does not prove every supplier behavior. |
| **US149** 作为管理员，我希望外部调用继续遵守取消、预算及脱敏约束，以便配置工厂收窄不会改变保护能力。 | V | `internal/metadata/provider_budget_test.go::TestProviderBudgetRespectsContextCancellation` | O+T; execution U; platform/device/provider U — Also TestRetryLoopHonoursContextCancellation, TestComicVineSearchDoesNotLeakAPIKeyOnTransportFailure, TestProviderBudgetLimitsConcurrentRequests. Actual remote provider calls unexecuted. |
| **US150** 作为维护者，我希望连接配置转换不要求构造整份全站配置，以便集成模块的实际依赖可以明确理解。 | V | `internal/config/llm_connection_test.go::TestLLMConnectionNormalizesLegacyEndpointWithoutApplicationConfig` | O+T; execution U |
| **US151** 作为维护者，我希望装配根独立创建应用、资源和宿主，以便 Controller 不再驱动整个进程构造。 | RT | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` | O+T slice; P47; final structural audit M48 — Independent production bootstrap exists, but legacy NewController construction/whole-store bridge removal pending47. |
| **US152** 作为维护者，我希望 HTTP 处理器按功能接收所需用例和查询，以便不会复制全部运行时引用或接受万能应用对象。 | RT | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` | O+T slice; P47; final structural audit M48; P46 — Reader/organization/library families integrated. Configuration/accounts adapter pending46; universal contraction pending47. |
| **US153** 作为管理员，我希望重启能力登记与一致性恢复先于后台和请求开放，以便恢复失败时服务不会暴露半完成状态。 | RT | `internal/bootstrap/bootstrap_test.go::TestFailedRecoveryReturnsNoHostAndOpensNoInputs` | O+T; execution U |
| **US154** 作为管理员，我希望停机先排空输入并等待使用者退出，再释放资源，以便后台收尾不会使用已关闭的池。 | RT | `internal/bootstrap/bootstrap_test.go::TestShutdownWaitsForHTTPConsumerBeforeStoppingEngine` | O+T; execution U — HTTP drain and SSE broker tested separately; no single production Host+live EventSource+active reader shutdown witness found in inspected bootstrap tests. |
| **US155** 作为维护者，我希望任务领域、任务落盘、传输投影、日志、事件和采样各保留含义，以便不会互相推导或混成万能事件。 | T | `internal/application/runtime/runtime_test.go::TestInterruptedCoverRunReplaysDeclaredDiscoveryThroughRegistry` | O+T; execution U — Projection owner separation observed; no one test proves all log/event/sample distinctions. |
| **US156** 作为维护者，我希望重启入参与声明标签严格分开，以便重启只重放影响行为的输入。 | T | `internal/application/scanning/cover_test.go::TestCoverRestartReplaysBackfillIndependentlyOfTrigger` | O+T; execution U |
| **US157** 作为管理员，我希望每次合并丢失不同重启入参都留下事件，以便强制等行为声明丢失时可以查明。 | T | `internal/task/events_test.go::TestEveryLossyCoalesceRetainsIncomingDeclaration` | O+T; execution U — Exact lossy-coalescence tests listed in task/taskstore appendix; default recovery test is supplemental only. |
| **US158** 作为管理员，我希望无损合并继续控制重复事件噪音，以便监听与守护不会制造大量无意义记录。 | T | `internal/task/events_test.go::TestRepeatedCoalescingLandsOneEvent` | O+T; execution U — Lossless event-noise tests listed in task/taskstore appendix; default recovery test is supplemental only. |
| **US159** 作为维护者，我希望发起方只说明谁发起，以便重启行为不再藏在触发类型中。 | T | `internal/application/scanning/cover_test.go::TestCoverRestartReplaysBackfillIndependentlyOfTrigger` | O+T; execution U |
| **US160** 作为维护者，我希望每个启动点的参数、标签、上限与指标有唯一归属，以便同一事实不在多个通道重复报送。 | T | `internal/application/runtime/runtime_test.go::TestInterruptedCoverRunReplaysDeclaredDiscoveryThroughRegistry` | O+T; execution U — Per-launch-site declaration ledger is a manual inventory obligation; not proved by one restart test. |
| **US161** 作为管理员，我希望任务身份、队列、全局槽位与卷级 IO 限额继续各司其职，以便整理不改变运行控制语义。 | T | `internal/task/pause_all_test.go::TestPauseAllHoldsTheQueue` | O+T; execution U |
| **US162** 作为客户端使用者，我希望运行 HTTP/SSE 契约和 TypeScript 生成结果保持兼容，以便类型归位不改变公开名称。 | T | `internal/api/task_push_frames_test.go::TestSnapshotFrameCarriesTheWholeRun` | O+T; execution U — Generated contract source and SSE tests exist; current drift check not run in this research. |
| **US163** 作为维护者，我希望传输模型与转换按功能组织，以便 Controller 中结构体减少对应明确归属而非污染领域。 | T | `internal/application/runtime/runtime_test.go::TestInterruptedCoverRunReplaysDeclaredDiscoveryThroughRegistry` | O+T slice; P47; final structural audit M48 — Family DTO boundaries partly migrated; compatibility file contraction pending47. |
| **US164** 作为维护者，我希望运行中的日志保留任务与运行上下文，以便迁移后仍可按运行查看故障与告警。 | T | `internal/application/scanning/cover_test.go::TestCoverWorkerLogsUseItsOwnRunAfterTheScanFinishes` | O+T; execution U — Logs require originating run context; scanner cover consumer test checks a real asynchronous handoff. |
| **US165** 作为维护者，我希望离线归属、下载、字节缓存和进度队列由完整离线功能拥有，以便鉴权与离线书架不反向依赖阅读器页面。 | OFF | `internal/reading/sync_test.go::TestReceivedFactsAtSameTickRetainRetryIdentity`; `web/src/features/offline/readerOfflineConsumer.test.tsx` :: finishes a download for the original book after navigation without assigning it to the next book | O+T; execution U |
| **US166** 作为阅读者，我希望断网与会话过期保留既有离线数据规则，以便暂时连不上服务不导致书目和进度丢失。 | OFF | `internal/reading/sync_test.go::TestReceivedFactsAtSameTickRetainRetryIdentity`; `web/src/auth/sessionExpiryOffline.test.tsx` :: 401 / session expiry retention suite | O+T; execution U — AuthProvider/session-expiry consumer tests exist; do NOT list auth-expiry retention as missing implementation. |
| **US167** 作为阅读者，我希望显式登出、换人和首次认领继续遵守既有所有权规则，以便本机离线数据不会串给另一用户。 | OFF | `internal/reading/sync_test.go::TestReceivedFactsAtSameTickRetainRetryIdentity`; `web/src/pages/book-reader/offlineOwner.test.ts` :: reconcileOfflineOwner ownership suite | O+T; execution U — Owner switch/logout tests exist including failed upload/logout; late ACK/new-owner test exists. Browser storage mocks are not device proof. |
| **US168** 作为离线阅读者，我希望重试、补传与回退保留真实时间及稳定事实身份，以便功能抽取不制造新阅读历史。 | OFF | `internal/reading/sync_test.go::TestReceivedFactsAtSameTickRetainRetryIdentity`; `web/src/features/offline/readerOfflineConsumer.test.tsx` :: reconnect uploads original facts for both books and an old acknowledgement cannot remove a new owner fact | O+T; execution U — Stable fact_id + occurred time and owner-generation fencing have producer/consumer tests; older page-directory test locations import new feature owner, not necessarily duplicated implementation. |
| **US169** 作为管理员，我希望配置编辑会话统一维护分区字段、草稿、保存基线和版本，以便分拆功能不会破坏并发保存。 | EDIT | `internal/api/config_consistency_test.go::TestConfigUpdatesSerializeSaveAndReload`; `web/src/pages/settings/configVersion.test.tsx` :: keeps edits made while saving and advances only to the acknowledged save baseline | O+T; execution U |
| **US170** 作为管理员，我希望保存一个分区不保存其他分区草稿，以便未确认的编辑不会被顺带提交。 | EDIT | `internal/api/config_consistency_test.go::TestConfigUpdatesSerializeSaveAndReload`; `web/src/pages/settings/configVersion.test.tsx` :: saves only the selected section and retains unsaved credentials across the refresh | O+T; execution U |
| **US171** 作为管理员，我希望刷新不替脏草稿换掉版本基线，以便冲突不会被悄悄掩盖。 | EDIT | `internal/api/config_consistency_test.go::TestConfigUpdatesSerializeSaveAndReload`; `web/src/pages/settings/configVersion.test.tsx` :: does not rebase a dirty section when an unrelated save refreshes an unchanged server section | O+T; execution U |
| **US172** 作为维护者，我希望 KOReader 账户诊断和维护动作独立拥有生命周期，以便它们不与配置草稿共用一个功能总对象。 | EDIT | `internal/api/config_consistency_test.go::TestConfigUpdatesSerializeSaveAndReload`; `web/src/pages/settings/settingsLifecycle.test.tsx` :: keeps the latest device diagnostics when earlier requests finish late | O+T; execution U |
| **US173** 作为管理员，我希望系列编辑草稿表达实际编辑意图，以便不在表单中构造数据库可空字段形状。 | M | `internal/application/seriesediting/manual_test.go::TestManualSaveReturnsCommittedMetadataAndPreservesAbsentCollections` | O+T; execution U |
| **US174** 作为维护者，我希望列表和详情可以有各自所需视图，以便不为同名数据强行合成全站实体。 | Q | `internal/reading/catalogue_view_test.go::TestCataloguePageBindsReaderBeforeFilteringCountingOrderingAndPagination` | O+T; execution U |
| **US175** 作为审核者，我希望 AI 分组动作返回后端权威候选与整单状态，以便前端不再复刻终态规则。 | AI | `internal/organize/ai_grouping_test.go::TestAIGroupingApplyCollectionOwnsIdentityAndAdjudication` | O+T; execution U |
| **US176** 作为审核者，我希望合并新审核结果保留滚动和选区，以便权威状态更新不打断操作。 | AI | `internal/organize/ai_grouping_test.go::TestAIGroupingApplyCollectionOwnsIdentityAndAdjudication`; `web/src/pages/aiGroupingReviewInbox.test.tsx` :: 终态离开 pending 筛选后仍消费动作返回的权威候选内容 | O+T; execution U |
| **US177** 作为维护者，我希望查询、普通重试与写入影响声明分开，以便调用 reload 不隐含宣称发生跨功能变更。 | FX | `internal/reading/mark_test.go::TestLaterMarkBatchFailureRetainsAndInvalidatesCommittedRange`; `web/src/session/catalogConsumers.test.tsx` :: ordinary series reload and retry do not declare catalog writes | O+T; execution U |
| **US178** 作为操作发起者，我希望写后刷新依据实际提交范围，以便部分失败也能看到已完成结果。 | FX | `internal/application/seriesediting/selection_test.go::TestSelectionRetainsCommittedMetadataWhenStatisticsFail`; `web/src/session/proposalConsumers.test.tsx` :: warns about committed %s with failed authoritative refresh, then retries only the query | O+T; execution U — Known prior proposal refresh swallowing gap was fixed (8e318f4). 16 parametrized tests are present; do not reopen from stale execution prose. |
| **US179** 作为维护者，我希望任务、气泡和外部传输的功能类型由功能拥有，以便它们不反向导入具体页面或组件类型。 | T | `internal/application/runtime/runtime_test.go::TestInterruptedCoverRunReplaysDeclaredDiscoveryThroughRegistry`; `web/src/features/task-observation/useTaskObservation.test.tsx` :: recovers expanded history and backend totals on gaps/reconnect while preserving filters | O+T; execution U |
| **US180** 作为管理员，我希望资料库表单提交与导航刷新归资料库功能，以便 Layout 主要持有入口和局部交互。 | L | `internal/application/libraries/save_test.go::TestCreateLibraryKeepsCommittedResultWhenInitialScanAdmissionFails`; `web/src/features/libraries/useLibraryManagement.test.tsx` :: keeps drafts and browser location after rejected save, then distinguishes committed save from failed navigation refresh | O+T; execution U |
| **US181** 作为阅读者，我希望零、空、未加载、失败和陈旧可区分，以便读取故障不冒充没有数据。 | UI | `internal/bootstrap/bootstrap_test.go::TestHTTPQueryCacheReceivesTheApplicationInvalidation`; `web/src/pages/QueryFailure.test.tsx` :: Dashboard shows genuine zero totals and explicitly empty lists after successful requests | O+T; execution U |
| **US182** 作为阅读者，我希望已有成功数据失败后可标为陈旧，首次失败提供重试，以便页面既可用又如实表达状态。 | UI | `internal/bootstrap/bootstrap_test.go::TestHTTPQueryCacheReceivesTheApplicationInvalidation`; `web/src/pages/QueryFailure.test.tsx` :: Dashboard preserves labelled stale overview while optional failures are retryable, then clears stale on recovery | O+T; execution U |
| **US183** 作为阅读者，我希望身份、资料库、系列、书或请求代次变化后迟到响应不能覆盖新视图，以便跨功能刷新不会破坏当前上下文。 | UI | `internal/bootstrap/bootstrap_test.go::TestHTTPQueryCacheReceivesTheApplicationInvalidation`; `web/src/pages/QueryFailure.test.tsx` :: recommendations in flight cannot populate the next identity | O+T; execution U |
| **US184** 作为维护者，我希望初始化、角色修改、删账户和改密都有完整用例，以便不变量不散落在 HTTP 和 Store 调用之间。 | ACC | `internal/api/setup_atomic_test.go::TestSetupAtomicallyCreatesOnlyOneAdministrator` | O+T; execution U; P46 — Account use cases integrated, final HTTP family migration pending46. |
| **US185** 作为站点所有者，我希望首次管理员建立遵守初始化条件，以便并发请求不能绕过站点状态。 | ACC | `internal/api/setup_atomic_test.go::TestSetupAtomicallyCreatesOnlyOneAdministrator` | O+T; execution U |
| **US186** 作为站点所有者，我希望最后一个管理员不能被删除或降级，即使请求并发也成立，以便不会失去管理能力。 | ACC | `internal/api/account_management_atomic_test.go::TestConcurrentAccountDemotionsPreserveAdministrator` | O+T; execution U |
| **US187** 作为管理员，我希望禁止删除自身的既有规则继续有效，以便结构迁移不改变管理保护。 | ACC | `internal/api/account_management_atomic_test.go::TestAccountManagementFreshAuthorizationAndSelfDelete` | O+T; execution U |
| **US188** 作为用户，我希望用户名继续按字节精确判定，以便已有独立账户不会被合并。 | ACC | `internal/api/setup_atomic_test.go::TestSetupSessionFailurePreservesCommittedAccountAndExactUsername` | O+T; execution U — Byte-exact account semantics; do not introduce case folding or username normalization. |
| **US189** 作为用户，我希望密码、会话撤销和鉴权缓存得到一致交接，以便改密后旧资格不会继续被误用。 | ACC | `internal/api/password_transition_test.go::TestPasswordCommitPreventsOldInFlightBasicCredentialsFromRepopulatingCache` | O+T; execution U |
| **US190** 作为站点所有者，我希望首次初始化时旧阅读数据与设备账户归属保留原有规则，以便完整用例抽取不遗失历史。 | ACC | `internal/api/setup_atomic_test.go::TestSetupClaimsLegacyActivityAndDeviceAccountWithoutMergingCredentials` | O+T; execution U |
| **US191** 作为用户，我希望协议账号和站点用户身份继续区分，以便设备匹配不会越过账户边界。 | ACC | `internal/api/password_transition_test.go::TestPasswordResetAuthorizationAndProtocolEligibility` | O+T; execution U |
| **US192** 作为维护者，我希望 Cookie、CSRF、限流和协议认证保留传输安全职责，以便账户领域不吸收全部 HTTP 细节。 | ACC | `internal/api/security_p0_test.go::TestSetupRoutesEnforcesSession` | O+T; execution U; P46 — Cookie/CSRF/Basic stay transport concerns; final administration extraction pending46. |
| **US193** 作为维护者，我希望账户规则使用真实事务验证，以便仅靠事务外计数不能被视为原子保证。 | ACC | `internal/api/account_management_atomic_test.go::TestConcurrentCrossAccountDeletesAndStaleActorKeepAdministrator` | O+T; execution U |
| **US194** 作为维护者，我希望通过完整功能接口和真实消费者验收，以便测试不会只镜像内部辅助方法。 | A | `internal/bootstrap/reader_http_test.go::TestBoundReaderPositionRatingAndProtocolConsumers` | M48 final joint evidence; representative O+T; execution U — Representative complete-use-case consumers exist; final all-story consumer audit is missing/pending48. |
| **US195** 作为维护者，我希望真实临时 SQLite 覆盖提交、回滚、竞争和重启，以便模拟仓储不能掩盖事务错误。 | A | `internal/organize/franchise_facts_test.go::TestFranchiseCascadeSourcesRecoverAfterReopenWithStableIdentity` | M48 final joint evidence; representative O+T; execution U — Real SQLite rollback/race/reopen tests present; final integrated execution pending48. |
| **US196** 作为维护者，我希望文件和资源测试覆盖暂停、取消、占用、替换与后续失败，以便只测成功路径不能掩盖数据风险。 | A | `internal/application/diskjobs/transfer_pause_test.go::TestTransferRunPauseResumeAndCancelKeepCommittedRange` | M48 final joint evidence; representative O+T; execution U — Temporary real files used; additional hardware/platform run is unexecuted, not inferred from source. |
| **US197** 作为维护者，我希望装配测试覆盖恢复前禁止开放与停机等待，以便模块化后承重时序仍可执行验证。 | A | `internal/bootstrap/bootstrap_test.go::TestShutdownWaitsForHTTPConsumerBeforeStoppingEngine` | M48 final joint evidence; representative O+T; execution U — Recovery failure/HTTP drain/resource retirement represented separately; SSE combined consumer witness still to verify. |
| **US198** 作为维护者，我希望前端通过功能消费者、路由和必要页面验证可见结果，以便不以 hook 调用次数代替正确行为。 | A | `internal/bootstrap/reader_http_test.go::TestBoundReaderPositionRatingAndProtocolConsumers`; `web/src/session/ApplicationRoutes.test.tsx` :: actual navigation routes share the session, refresh the library and release it in reader and graph | M48 final joint evidence; representative O+T; execution U — Route and necessary-page tests present; complete approved story-to-consumer final audit pending48. |
| **US199** 作为维护者，我希望保留必要 HTTP 和设备协议回归，以便模块测试通过时外部使用方式仍兼容。 | A | `internal/bootstrap/reader_http_test.go::TestBoundReaderPositionRatingAndProtocolConsumers` | M48 final joint evidence; representative O+T; execution U — HTTP/OPDS/Mihon/KOReader regressions present; fresh integrated protocol pass not established. |
| **US200** 作为维护者，我希望所有主要区域有规格到实现及执行证据映射，以便工单完成不能代替整个项目的归属验收。 | A | No final acceptance artifact/test PASS established; representative `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` | M48 final joint evidence; representative O+T; execution U — This external research inventory is a draft input, not the final repository evidence artifact or completion of48. |
| **US201** 作为维护者，我希望格式、lint、生成契约和多平台门禁通过，以便移动符号和类型不会留下构建或漂移问题。 | A | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` | U current gates/multiplatform; historical evidence only — Current-tree full tests/lint/format/generator drift/race/multi-platform gates UNEXECUTED in this read-only task; historical logs are scoped below. |
| **US202** 作为维护者，我希望删除旧副本及无人调用桥并更新当前文档，以便交付后不会留下两份权威或过期实施状态。 | A | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` | M48 final joint evidence; representative O+T; execution U — Pending47 removal of old host/forwarders; documentation current-state reconciliation pending48. |
| **US203** 作为使用者，我希望变更记录说明批准的行为纠正，以便升级时可以理解冲突、失败和恢复的新结果。 | A | No final acceptance artifact/test PASS established; representative `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` | M48 final joint evidence; representative O+T; execution U — CHANGELOG has accumulated approved corrections; final checklist against every approved behavior is missing/pending48. |
| **US204** 作为维护者，我希望区分未实现、测试存在、实际通过和未执行限制，以便规格就绪不会被当成运行成果。 | A | No final acceptance artifact/test PASS established; representative `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` | M48 final joint evidence; representative O+T; execution U — Evidence taxonomy supplied here; final execution proof and all limits still must be recorded by48. |

## Major-area disposition (not exhaustive file review)

| Area | Observed disposition / depth | Remaining acceptance work |
|---|---|---|
| cmd/server, bootstrap, API/protocol | Adjusted independent composition and reader/organization/library families; bootstrap/host critical paths read |46 admin/security,47 legacy mirrors; final bound-host parity |
| reading, catalogsearch, seriescontext, recommendation, seriesreview, dashboard | Retained batched SQL and query-specific views; adjusted Subject/write boundaries; source/symbol inventory | SQL pagination/cache subject keys, real failed/stale/empty consumers; not every SQL branch deep-read |
| seriesmeta, proposal, organize | Retained source policies/member semantics; adjusted complete metadata/adjudication/repair ownership | Final concurrency/reopen/cascade/cache handoff execution |
| libraries, task admission, relocation | Adjusted complete management, same-Engine reserve/drain and persistent move process; critical deletion path read | Actual-exit/queued/cross-library consumers, recovery after47 |
| bookcontent, parser, images, external, storageio, diskwork | Retained technical format/processing/quota responsibilities; adjusted shared content/publication/copy; critical file tests read | OS replacement/no-clobber, pause quota and cancel/post-commit consumers; platform U |
| scanner, bookidentity, KOReader | Adjusted neutral evidence and ingestion/watcher cleanup qualification; retained protocol and matching algorithms | Actual device U; ambiguity/root protection/lock policy and cover handoff |
| config, runtimecfg, metadata, intelligence | Retained Manager/provider adapters, adjusted publication/timing/resource/narrow ports |46 wiring, compensation and old-resource return; real provider/hardware U |
| task, taskstore, runhandle, taskcontrol, logger | Retained domain/storage/control/log distinction; adjusted declarations/observation | Per-launch input/label/limit/metric manual ledger; logger only indexed, not exhaustive deep-read |
| accounts, auth/browser session | Adjusted transactional invariants/password revocation; retained Cookie/CSRF/Basic transport |46/47 final routing security and in-flight cache regression |
| web session/features/pages | Adjusted offline/edit/library/task ownership/query effects; real consumer/route/page tests inventoried | Identity/generation and commit-refresh consumers; no hook-count substitute |
| SQL/schema/generated Go/TS | Retained SQLite/sqlc/public JSON/TS; generator obligations identified | Fresh applicable migration/PowerShell sqlc/tsgen drift verification U |
| format algorithms, i18n/theme/CSS/assets/tooling | Retain justified technical ownership; largely index-only here | No unrelated rewrite; approved behavior/gates still apply |

No area is completed merely by appearing in this table. `audit.md` historical candidate descriptions are not current defects. ADR0008 and0010–0015 current-state headers and CHANGELOG v1.8.0 were inspected:0014 explicitly says phased migration and describes all3 integrated families; final47/48 current-state reconciliation remains open. CHANGELOG already describes proposal conflicts, durable repair, deletion, publication, pause/cancel, auth handoff, scoped stale states and committed-refresh corrections; missing evidence is its final approved-behavior checklist, not an absent changelog.

## Supplemental exact test-symbol inventory for high-risk clauses

These close searchability gaps in compact rows. Their presence is NOT execution evidence. Select only semantically applicable tests; not every test in a file proves every requirement.

- `internal/parser/borrow_test.go`: `TestArchiveBorrowCountIncludesDirectAndEvictedPoolReaders`, `TestFormatSpecificReadersParticipateInArchiveBorrows`, `TestArchiveBorrowCountSurvivesPoolReplacementAndExcludesIdleHandles`
- `internal/parser/parser_test.go`: `TestPoolEvictionDoesNotCloseBorrowedHandle`, `TestCoverKeywordMatchingIsWholeToken`
- `internal/parser/publication_test.go`: `TestComicInfoPublicationWaitsForEvictedBorrowAndBlocksNewReaders`, `TestComicInfoPublicationTimeoutCancelAndSourceChangePreserveOriginal`
- `internal/task/await_test.go`: `TestAwaitWakesWhenAQueuedRunIsCancelled`, `TestAwaitReleasesOnContextCancel`
- `internal/task/backoff_test.go`: `TestCancelAndInterruptDoNotCountAsFailures`
- `internal/task/events_test.go`: `TestCoalescedLaunchLandsOnTheQueuedRun`, `TestRepeatedCoalescingLandsOneEvent`, `TestEveryLossyCoalesceRetainsIncomingDeclaration`
- `internal/task/library_drain_test.go`: `TestLibraryDrainCancelsOnlyEligibleIdentityAndActualCoverage`, `TestReleasedLibraryReservationCannotCancelNewWork`
- `internal/task/library_reservation_test.go`: `TestLibraryReservationWaitsForBodyExitDespiteTerminalSnapshot`, `TestLibraryConflictsUseIdentityAndCoverageForCancellationEligibility`
- `internal/task/resume_test.go`: `TestRestartResumesWhitelistedTypesOnly`, `TestPausedAndCancellingRunsAreNotResumed`
- `internal/task/slots_test.go`: `TestRepeatedStartsCoalesceIntoTheQueuedRun`, `TestCoalescedRunOnlyRunsOnce`, `TestCoalescingIsDecidedByTheStore`
- `internal/task/start_test.go`: `TestTriggerMustBeDeclared`
- `internal/task/state_machine_test.go`: `TestCancelFromPausedReleasesTheGate`, `TestCancellingIsStillActive`, `TestQueuedRunCancelsStraightToCancelled`, `TestRestartMovesLiveRunsToInterrupted`
- `internal/task/stop_test.go`: `TestStopAllCancelsBeforeResuming`
- `internal/taskstore/read_back_test.go`: `TestQueryMatchesScopeName`, `TestCoalescingPersistsDiscardedDeclarationsWithoutReplacingQueuedBody`
- `internal/taskstore/task_list_test.go`: `TestListTasksMatchesIdentityAndKeyword`, `TestListTasksMatchesLastRunScopeName`
- `internal/application/scanning/cover_test.go`: `TestCoverPauseCancellationAndRestartBackfill`, `TestCoverRestartReplaysBackfillIndependentlyOfTrigger`
- `internal/application/intelligence/intelligence_test.go`: `TestQueuedGenerationCancellationReleasesCoverageWithoutCallingSource`
- `internal/application/maintenance/maintenance_test.go`: `TestRestartPreservesForegroundMatchingArguments`
- `internal/config/config_extra_test.go`: `TestNormalizeConfigKOReaderBasePathAndMatchMode`, `TestSnapshotIsDeepCopy`
- `internal/config/effects_test.go`: `TestFieldTimingCatalogCoversConfigurationAndKeepsStartupFieldsExplicit`
- `internal/config/runtime_test.go`: `TestManagerSnapshotAndReplace`, `TestNormalizeConfigDefaultsResumeAfterRestartToOn`
- `internal/metadata/comicvine_test.go`: `TestComicVineRedactsAPIKeyInUpstreamError`
- `internal/metadata/errors_test.go`: `TestProviderLogsNeverEmitOversizedUpstreamBody`
- `internal/metadata/provider_budget_test.go`: `TestProviderBudgetKeysMatchRealProviderNames`, `TestProviderBudgetLimitsConcurrentRequests`, `TestProviderBudgetRespectsContextCancellation`, `TestProviderBudgetPassesThroughUnlistedProviders`
- `internal/metadata/retry_loop_test.go`: `TestRetryLoopHonoursContextCancellation`
- `internal/koreader/fingerprint_extra_test.go`: `TestHashKeyTrimsAndMatchesMD5`, `TestFingerprintDocumentPathNormalization`, `TestFingerprintRelativePathMatchesDocumentPath`, `TestNormalizeDocumentForMatchModes`, `TestReadableMatchMode`, `TestFingerprintFileMatchesMD5AndErrors`, `TestFingerprintQuickFileHeadTailSampling`
- `internal/koreader/fingerprint_test.go`: `TestFingerprintQuickFileChangesWithTailContent`, `TestFingerprintQuickFileSmallFiles`, `TestFingerprintFileContextStopsOnCancel`
- `internal/koreader/position_test.go`: `TestMatchedDevicePushAllowsBackwardCurrentPosition`, `TestMatchedPullUsesOriginalPreciseDeviceOccurrence`, `TestMatchedSamePageDeviceReportRefreshesTimeWithoutCrossingUsers`, `TestDeviceReplayAfterClearAndRestartCannotRestoreOldPosition`, `TestMatchedDeviceActivityWarningIsNotAPositionFailure`
- `internal/koreader/service_test.go`: `TestSaveProgressRejectsOversizedFields`
- `internal/images/processor_avif_test.go`: `TestProcessImageAvifThumbnailStaysFast`
- `internal/images/processor_process_test.go`: `TestProcessImagePassthroughWhenNoOps`, `TestProcessImagePassthroughWhenFormatMatches`, `TestProcessImageReencodesWhenFormatDiffers`, `TestProcessImageFilterWithoutResizePassesThrough`, `TestProcessImageFilterAppliesWhenResizing`, `TestProcessImageFitInsidePreservesAspect`, `TestProcessImageFitInsideDoesNotUpscale`, `TestProcessImageImagingFiltersWithSingleDimension`, `TestProcessImageResizeExactDimensions`, `TestProcessImageResizeWidthOnlyPreservesAspect`, `TestProcessImageFormatInheritance`, `TestProcessImageEncodesWebP`, `TestProcessImageErrorOnNonImage`, `TestProcessImageRejectsDecodeBomb`, `TestFormatMatchesContentType`, `TestFlattenImageNil`, `TestFlattenImageAlreadyOrigin`, `TestFlattenImageNormalizesOffset`, `TestAutoCropImageTrimsBorder`, `TestAutoCropImageKeepsTinyImages`, `TestProcessImageRejectsUnsafeTargetDimensions`, `TestProcessImageAcceptsReasonableTargetDimensions`, `TestProcessImageAIFailureDoesNotFakeUpscale`, `TestProcessImageFitInsideNoOpPassesThrough`, `TestProcessImageCanvasResizeStillUpscales`
- `internal/images/processor_semaphore_test.go`: `TestProcessImageConcurrentSoftwareEncodesComplete`
- `internal/images/processor_stride_test.go`: `TestFlattenedSubImageSurvivesEveryEncoder`, `TestFlattenImageNormalizesNonCompactStride`, `TestProcessImageAutoCropToAvifKeepsPixels`

## Historical execution references (not current-tree passes)

- Fixed original baseline in execution.md: `badc595541250189586b87e9d1c1c83ac79fd677`; baseline Go full run did NOT pass. Initial frontend retry and later package reruns are explicitly distinct from one green baseline command.
- 01/08/10 closure `e0f7d1b`;02 `20bc0b5`;13 `1871f05`;03/09 `b6a701d`;04 `4d49045`;14 `f31aca1`;05/16 `ceda2d1`;06 `096b8cc`;17 `b4c80a0`;07 `a078803`;22 `8852105`;18 `1e4c436`. execution.md contains scoped ordinary/race/UI summaries. Exact source tree per original log needs checking before final acceptance citation.
- Real-file/coordination historical logs: `.tmp/merge-wave9-*.log` (11/23/19), `.tmp/merge-wave11-*.log` (20/15), `.tmp/merge-wave14-*.log` (26/21), `.tmp/merge-wave21-*.log` (31/28). Launch blocks, Windows rename Access denied, Unix symlink skips and external race launch restrictions are explicitly not passes; targeted retries have their own scope.
- Account histories: `.tmp/merge-wave16-account-*.log` (38), `.tmp/merge-wave18-*.log` (39), `.tmp/merge-wave20-*.log` (40). Owner packages can have no independent `_test.go`; real SQLite HTTP fixtures directly invoke use cases. Package compilation is not behavioral evidence.
- 25/33 historical combined run initially FAILED scanner cleanup and restoration-fixture race; fixes recorded `6c7d587`. Later `.tmp/feature-combined-*.log` records complete Go ordinary test success plus focused repeated races. This is not a full API race success.32 `.tmp/settings-integration-web-*.log`;42 `.tmp/bootstrap-combined-integration-race.log`;34/35 `.tmp/frontend-feature-combined-*.log`.
- 33/34/35 post-merge review targets `0b7e2e7`; review explicitly did NOT rerun tests.37 `.tmp/declarations-combined-*.log` describes historical complete ordinary Go + seven-package race and frontend1008.34 follow-up `.tmp/proposal-combined-*.log` records1024 frontend tests; final proposal fix integration commit `8e318f4` is in current ancestry. Full API race timeouts and broader backend timeout remain NOT PASS.
- 43 synchronized tree `.tmp/t43-synchronized-*.log`;43/44 combined merge evidence in44 tree `.tmp/merge4344-*.log`: HTTP/bootstrap/cmd, focused families/bootstrap races, fmt/lint/build and26 generated types. Issue43/44 Status metadata remains stale. execution.md trailing44 resumed-worktree paragraph says not integrated, but is older than parent-confirmed integration. Retain original provenance, do not use old prose to undo integrated code.
-45 issue `.tmp/45-api-focused-race.log` records focused API416.384s; `.tmp/45-owners-race.log` scoped owner packages. Whole-repo race exceeded600s and full API retry timed out at240s; neither is full PASS. Observed HEAD `7589b15` integrates45 after `85b1809` composition merge. Need parent/merge logs for final45 tree verification; issue worktree evidence alone is not a current HEAD gate.
- Raw integrated logs advertised under `.tmp/merge-*.log`, retired-worktree logs under `.tmp/evidence/`. This research does not open huge logs, validate all counts, reconstruct missing log commits, or extrapolate their results to final47/48 tree.

## Documented scope exclusions versus genuine gaps

- Not gaps: no microservices/multiple contexts; no uniform domain/application/infrastructure template; no universal repository/UoW; no global app/store/event bus, saga, outbox or second task engine; no forced map of every SQL row to an entity; retained technical helpers/singletons are allowed.
- Not gaps: no new hash/path/size+mtime identity algorithm, no heuristic merges/history invention; no conversion of soft reading statistics into hard rollback; no source-policy normalization; no cross-library external transfer; no cross-process automatic transfer resume; no forcibly cutting downloads or OS-blocked IO; no cross-tab connection merger or whole-site offline task system; no replacement auth/permission system, provider redesign or unrelated CSS/format algorithms.
- Genuine remaining predecessor obligations:46 narrow admin/config/account dependencies,47 old-constructor/full-store/service-mirror/forwarder contraction. Genuine acceptance gaps: final-tree consumer evidence for combined shutdown and post-commit downstream outcomes; all-story manual ownership/majority-area review; executed final gates/contract drift/platform evidence or explicit limitations. Missing evidence is not automatically a discovered production bug.
- No interview or product decisions requested; all checks use approved contracts. If48 finds a failing behavioral clause, route to that existing owner ticket with exact failing consumer, rather than broadening48 into a new universal framework.

## Read manifest / reproducibility

All48 issue files were read in full by this inventory process; SHA-256 below fingerprints the observations, not completion or execution. Root/scratch docs were read via tools. Production/test pointer outlines above were obtained from actual source; test names were resolved against repository files (failure to resolve is fatal for Go pointers).

- `01` `.scratch/architecture-refinement/issues/01-reader-book-view.md` sha256 `948149850df72ddfb6fac36399f9a4c524d7e0d7eacc013604807acb18c4964d`
- `02` `.scratch/architecture-refinement/issues/02-catalog-reader-pagination.md` sha256 `93fe8beed39ab0dc65899cd64943e81617ba698788d55a65c7ac617f9ebb55c9`
- `03` `.scratch/architecture-refinement/issues/03-organized-reader-queries.md` sha256 `56a56b49bbaa86ec245ee4f20f988180d34977bbb28877a4c5a8f59d28001b0d`
- `04` `.scratch/architecture-refinement/issues/04-protocol-reader-queries.md` sha256 `9698fd74043e194fecff4683860ea54588ccf37dc2bc4a3fc7f4d0c29b2ad930`
- `05` `.scratch/architecture-refinement/issues/05-reading-write-transactions.md` sha256 `af124c7a059b78bf81f4c603a10d56d717d981c232b95fd18a10c6a71ba5803d`
- `06` `.scratch/architecture-refinement/issues/06-reader-time-bookmarks.md` sha256 `07a266a95db2bd50adb59322bf42a7244f95259e7efb90d87c5391909788b940`
- `07` `.scratch/architecture-refinement/issues/07-reader-model-contract.md` sha256 `c8eafe6c061bb814a464e212fdbf3f608a04a668d416b4ad59326fc629bba6de`
- `08` `.scratch/architecture-refinement/issues/08-manual-metadata-usecase.md` sha256 `ca6d6fcfbaf7b5dd218c0563b0997d3aa9471df3fb5d59c9471d12503abfc69a`
- `09` `.scratch/architecture-refinement/issues/09-bulk-metadata-usecase.md` sha256 `5e0d4f230fcae337f35b16cc04b52f5afa0a3c6d955bb2c6ff9103db02473eea`
- `10` `.scratch/architecture-refinement/issues/10-proposal-concurrency.md` sha256 `ca811b6173f461bedac1156b69ef13bcc55038461f8fb1a44448ac23dd41315b`
- `11` `.scratch/architecture-refinement/issues/11-collections-snapshot.md` sha256 `41e301d41496edc2a64be2db3b967085811b092bb6d065cb065ea7f53d452177`
- `12` `.scratch/architecture-refinement/issues/12-ai-review-authority.md` sha256 `a4fdc1f52bcf78b063cb54924a53a45effce5b667d07cb1e9c39ff372d4d0ec5`
- `13` `.scratch/architecture-refinement/issues/13-franchise-durable-repair.md` sha256 `b7748db0fc86a01ca032b551af1dae9ae024b63c5dd1eb4bf720c092b9170658`
- `14` `.scratch/architecture-refinement/issues/14-franchise-cascade-facts.md` sha256 `93c7866f5f0499ea0ab5ec3e6d1f37d418af688d25611b00f453027a6d67159d`
- `15` `.scratch/architecture-refinement/issues/15-ordered-reading-list-writes.md` sha256 `2e40e2e744aad43d8cb79b9f9a8345401479406256bb3e3083bd6feb3a8bf401`
- `16` `.scratch/architecture-refinement/issues/16-book-content-pages.md` sha256 `53ebf617a4b6125a07fda93ef752b080d14cd25277560148b80094246dae336a`
- `17` `.scratch/architecture-refinement/issues/17-book-download-handles.md` sha256 `c512aa2130fd37612d61b624b89cfe3bcac70b672d461b826f8bf84ce9bceffc`
- `18` `.scratch/architecture-refinement/issues/18-comicinfo-publication.md` sha256 `e912415c0e1b07df49b0d13d0cbae1080b2364811c8b4bac404b66332eb57b81`
- `19` `.scratch/architecture-refinement/issues/19-external-root-boundary.md` sha256 `306a1db43c9eff9204c546083b5b23ada4aab641ae727dad2a52754558018630`
- `20` `.scratch/architecture-refinement/issues/20-transfer-cancellation.md` sha256 `5fdcdb199e85edc8e808b2d53a65b5589b7f77e63bc11d676bb0c8224c5c0b75`
- `21` `.scratch/architecture-refinement/issues/21-transfer-pause-resume.md` sha256 `dcc2529d71150134c275a21f1bc8aa009a0cf0e563d724515994cad28ed08fe8`
- `22` `.scratch/architecture-refinement/issues/22-library-delete-reservation.md` sha256 `5fd03108a129ac6280724460d0117dd7f565bca6902bcd550a989b567b44fe15`
- `23` `.scratch/architecture-refinement/issues/23-library-delete-drain.md` sha256 `7f889a34ead20631658b3822162b6065cd55c0529f8e445297396f3d46ccea1b`
- `24` `.scratch/architecture-refinement/issues/24-library-create-update.md` sha256 `ae8221c5fed4a5c9ed3dd7df258698f96a86479c5c658c81e358b5a8e6771345`
- `25` `.scratch/architecture-refinement/issues/25-relocation-process-storage.md` sha256 `c9b27758c75b4f2ce604426bf8e0b9a9a841e38a7410d21dfa27dc10917a319f`
- `26` `.scratch/architecture-refinement/issues/26-book-identity-matching.md` sha256 `cfb72918dabaf9c7240876388c7f14443af5d823c0ec69453eb44e00eae3ec12`
- `27` `.scratch/architecture-refinement/issues/27-scan-ingestion.md` sha256 `814b934394d6c279bdc3ff228fe7e3a03d1b3f9dc62d794eecede31bcb142dc1`
- `28` `.scratch/architecture-refinement/issues/28-watch-ingestion-coordination.md` sha256 `8f8d0fbc3c3081634cbe5639c784afe64e12af8dc4e114b81303368f31841287`
- `29` `.scratch/architecture-refinement/issues/29-metadata-consumer-ports.md` sha256 `8e921d4c2e74d0986176f7387c2b215c52fbf130eae9b42cb7cd88731d8035da`
- `30` `.scratch/architecture-refinement/issues/30-configuration-effects.md` sha256 `13405f79c16efe26b3591146bef5fdb77742661bf1a76b65197f7e1c1301b6d2`
- `31` `.scratch/architecture-refinement/issues/31-runtime-resource-ownership.md` sha256 `f9c83fa7e3bf7820fa0fed49de546e9598788ecae67bb7abd3a40068ec6044be`
- `32` `.scratch/architecture-refinement/issues/32-settings-edit-session.md` sha256 `1fbd458dee1af4a0607be67c9428b60ee90049f08294dd2f53343b587b10e33a`
- `33` `.scratch/architecture-refinement/issues/33-offline-feature.md` sha256 `1ff6933d7a718d95f406da905b859aefae37af8c55b8d3216946e483205dac73`
- `34` `.scratch/architecture-refinement/issues/34-catalog-query-action-effects.md` sha256 `89a618a8ac909cf8c7a4fc9c2eef98a1399b875d35d265ad9262aca97ed0dcee`
- `35` `.scratch/architecture-refinement/issues/35-query-failure-ui.md` sha256 `064bf6ce73abb9216e5899a6c2f7639d67bbcd592411466ae1d0e1cfffcc75d5`
- `36` `.scratch/architecture-refinement/issues/36-task-observation-contracts.md` sha256 `e342e66ff74a8c93842ea7ecc561459cb9c5b9e4afbc4ec9c857bb26484cd648`
- `37` `.scratch/architecture-refinement/issues/37-run-declarations.md` sha256 `496283bb6f04fc273c8695c8ca78c8cf4cbe135ec9fbd8ed1916a2f5222c0494`
- `38` `.scratch/architecture-refinement/issues/38-account-initialization.md` sha256 `aa324ee964bc57a7f5ed7a8e570e5007b85fb170016bcd46ead7e6dc6dd25fac`
- `39` `.scratch/architecture-refinement/issues/39-account-administration.md` sha256 `fbe176baab95c0f8109736b8289e2da99e74722034a91f5ac3825956b53677f5`
- `40` `.scratch/architecture-refinement/issues/40-credentials-sessions.md` sha256 `aab45487e586d806c12e21c33ace78a741f74fd8a894916a379234b5a8a1b7ad`
- `41` `.scratch/architecture-refinement/issues/41-composite-queries-cache.md` sha256 `aa1d497005c2ba5e841150f638bd91f09b093dfa0ae8ce2b2c79c930f1342927`
- `42` `.scratch/architecture-refinement/issues/42-bootstrap-expand.md` sha256 `b8cb7d1061c5427b1d343d5890ceabfd152a1c0715b631d5f3586a45ea05161c`
- `43` `.scratch/architecture-refinement/issues/43-http-reader-query-migration.md` sha256 `938eafc59cce061dc23924879b80c855714a0a8afbf3227fbe3526de23291aab`
- `44` `.scratch/architecture-refinement/issues/44-http-organization-migration.md` sha256 `125ddac9e1ee0e0d526f8a64c8257b0af245cf915a5160e2a970a8700a40cbf3`
- `45` `.scratch/architecture-refinement/issues/45-http-library-runs-migration.md` sha256 `e0ee3b1e96f1a6036f53719138e3a2f746b96fe1cd094d1f4ff14d8fca3cf55b`
- `46` `.scratch/architecture-refinement/issues/46-http-config-accounts-migration.md` sha256 `8acdecbd81f95395ab8b74420bbbf37888d64e79e342747c764ed7d3fcc5e308`
- `47` `.scratch/architecture-refinement/issues/47-bootstrap-contract.md` sha256 `507ffcc5170fd2be4b3240ad1af9c1542c864d77f57f67a4071ad781b4f36581`
- `48` `.scratch/architecture-refinement/issues/48-integrated-acceptance.md` sha256 `41ba86dfb922e6788984ba40de38a444a0a3e0cb53bdd70521b7a13ead42ed28`
