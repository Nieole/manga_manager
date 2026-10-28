# 26: 书身份与设备匹配能力

**What to build:** 扫描和维护通过中立证据能力获取指纹，KOReader 保留协议账号与设备匹配职责。

**Blocked by:** None (can start immediately)。

**Status:** done

**规格覆盖：** [父规格](../spec.md) US 130、131、132、133、134、191。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 分别表达书身份、内容证据和设备文档键，不因移包改口径。
- [x] 扫描、维护和设备同步的生产消费者迁移，不为新接口增加空转发。
- [x] 保持哈希、路径、字节身份及 size+mtime 重连歧义取舍。
- [x] 真实文件证据与设备协议矩阵验证重建、匹配及兼容身份。

## Comments

### Implementation and verification — 2026-10-28

- `bookidentity` owns full-byte MD5, size/head/tail quick SHA-1 and normalized relative-path evidence, with distinct `ContentHash`, `QuickHash` and `PathHash` types. Persistent book IDs remain database identities; evidence equality does not authorize merging records. `koreader.DocumentKey` expresses a protocol lookup separately from those evidence values and from account sync keys. `CONTEXT.md` records the vocabulary through the domain-modeling skill.
- The complete evidence rebuild loop moved from `koreader.Service` to the narrow-store `bookidentity.Service.Rebuild`. It retains batch cursors, actual disk admission/checkpoints, unreadable-file skipping, cancellation, gaps and caller-retained matching mode. Maintenance calls this owner directly for rebuild, background backfill and the first stage of matching refresh. Scanner consumes neutral evidence functions directly. Old KOReader fingerprint/rebuild producers were removed; no protocol-to-evidence forwarding layer remains.
- KOReader retains account registration/authentication, constant-time credential checks, device document normalization and matching, receipt persistence and reconciliation. MD5 credential derivation stays in `koreader/credentials.go`. HTTP maps typed device lookup keys to the existing JSON strings. Algorithms, path normalization, hash bytes, enabled-library matching and the scanner's size+mtime/ambiguity/old-file-exists policies are preserved; scanner ingestion organization is outside this ticket.
- TDD at the approved complete seams: neutral evidence and rebuild interfaces were absent before their first tests (`.tmp/ticket26-red-evidence.log`, `ticket26-red-rebuild.log`). Fixed literal fixtures verify full MD5, quick SHA-1 and relative path digests over real files. A real temporary SQLite rebuild verifies the original book IDs, evidence writes, unreadable tail and advancing batch cursor. Existing real scan/rename/cleanup and KOReader credential/position/replay matrices now consume the neutral capability.
- Actual passing executions: scanner full 17.188s, maintenance full 2.377s and runtime full 8.085s (`ticket26-full-packages.log`); neutral owner full race 5.730s, KOReader full race 40.366s and maintenance full race 9.663s (`ticket26-owner-protocol-race.log`). Backend `go build ./...` passed and lint reported 0 issues. The binary-hash HTTP JSON consumer explicitly passed 6.204s after adapting the assertion to JSON's string boundary.
- Preserved failed evidence: several ordinary neutral/KOReader temporary executables were blocked by Windows Application Control, and those attempts are not passes. The initial full API execution failed 107.036s only because a JSON `map[string]any` string was compared directly with the new named content-hash type; the wire value was unchanged. The assertion now compares at its string boundary; full API retry and final integration checks follow below.
- This is an ownership/typing change with compatible visible behavior, so CHANGELOG has no new behavior entry. No frontend sources, SQL source or generated response structs changed.
- Status remains ready-for-agent until merger verification and closure.
- Scanner rename/identity race regressions also executed and passed 9.756s (ticket26-scanner-identity-race.log), including size+mtime ambiguity, content-hash preference, one-row claiming and preservation through cleanup. Full API retry is recorded separately; scanner watcher-stop behavior belongs to ticket 28 and is not claimed repaired here.

- Full API retry actually passed 115.324s (ticket26-api-full-fixed.log). Merged integration ff58410 in 4f5ef14 without conflicts. After-merge actual executions passed: bookidentity 2.584s, maintenance 3.373s, runtime 6.481s and the complete KOReader race suite 32.035s (ticket26-merged-packages.log, ticket26-koreader-merged-race.log). The ordinary KOReader executable in that combined command was blocked, so only its actual race retry counts. Merged lint reported 0 issues and gofmt output was empty. The complete baseline diff check and tracked conflict-marker scan are clean.

### Integration verification (2026-10-28)

- Merged through 3782d81f53fd382a123b79629c0be96f7ff13d38 without conflicts. Main-tree full race suites actually passed: bookidentity 4.845s, KOReader 46.562s, maintenance 16.574s and runtime 20.573s. Scanner rehome/identity/rename race passed 15.011s; KOReader device HTTP race passed 21.118s.
- Go 1.26.10 lint reported 0 issues and gofmt emitted no files. Whole-baseline git diff --check and tracked conflict-marker scans passed. Logs: .tmp/merge-wave14-owner.log, merge-wave14-scanner.log, merge-wave14-api.log and merge-wave14-lint.log. No frontend or generated contract target changed.
