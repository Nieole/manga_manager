# 48: 全项目联合验收与归属证据

**What to build:** 逐条核对全部规格、实际消费者和跨功能结果，记录通过及限制并完成文档与变更说明。

**Blocked by:** 33: 离线归属与补传功能；34: 查询与已提交操作影响分离；35: 看板与统计的失败陈旧呈现；47: 删除旧宿主与全量依赖兼容面。

**Status:** done

**规格覆盖：** [父规格](../spec.md) US 1、2、3、4、5、6、7、8、9、10、11、12、13、14、194、195、196、197、198、199、200、201、202、203、204。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 将全部故事映射到实现、消费者、实际执行及结构人工核对，不以票 done 代替结果。
- [x] 联合验证主体、事务、派生重启、访问排空、文件暂停/取消/发布、配置与装配时序。
- [x] 核对多数主要代码区域处置、旧规则副本退出、文档和 ADR 当前状态，记录可见行为变更。
- [x] 执行适用测试/格式/lint/生成漂移/竞态及多平台门禁，保留原始证据并区分未执行限制。
- [x] 本票不兜底实现前票遗漏功能；发现未满足条件回到对应功能票修复。

## Comments

### Retry acceptance artifact (2026-11-03; OPEN, uncommitted)

Preserved the interrupted attempt's complete `.tmp` logs and partial untracked mapping; target remains `codex/refinement-48` at `46c20cb`, no source changes. [acceptance.md](../acceptance.md) now maps **US001–US204 / 204 rows** to current implementation, real consumers, exact executed tests and 25 manual boundary records, with original-ticket routing for any future reproduced omission. Repository-owned canonical-text evidence is in [acceptance-evidence/](../acceptance-evidence/); commands, exits, JSON test results, source-tree digest and checksums distinguish execution from source/test presence.

Verified interrupted gates on unchanged production tree: complete ordinary Go PASS; complete all-package race PASS (API **723.912s**); vet/fmt/lint **0 issues**/native build PASS; TS **26 types** and SQL generation/no drift PASS; full frontend **1024**, lint/TS/build and vulnerability scan PASS. Retry freshly re-executed the full 1008+16 frontend split with concrete JSON case evidence, replacing three inherited suite/template pointers. Bounded traced doc-style retry completed **exit 0** after prior **180s timeout** (retained). Linux/amd64 and Darwin/arm64 CGO server cross-build attempts **failed** for missing target C toolchains; neither cross-build success nor non-Windows runtime is claimed. Real devices/providers/browser storage/hardware remain unexecuted. Older 47 complete API race **638.934s** and combination logs are cited only with their owning historical tree.

Current ADR 0014/CONTEXT wording reconciled; existing CHANGELOG already covers actual approved user behavior. No predecessor feature implementation added, no newly reproduced originating-ticket implementation gap established. All checkboxes above deliberately remain open: the artifact is ready for **subsequent independent behavior/spec/evidence and architecture/ownership reviews**, not their substitute. Parent owns review disposition, fixes/reopening of original tickets, and closure/integration.

### Evidence-integrity / committability correction (2026-11-03; OPEN)

The parent staged the six acceptance artifact paths, but `git diff --cached --check` failed on CRLF in new text; **no commit was created**. The earlier worktree-only diff check missed then-untracked files and was not a committability gate. The repair uses `dos2unix` to canonicalize CRLF to LF and precise edits to remove surplus empty EOF lines found by full-new-file Git whitespace checks in three logs, without dropping nonempty log lines or changing outcomes. Original `.tmp` captures remain untouched; the evidence README records the exact transform and original identities of affected captured gate logs. Retained text is canonical evidence, not a claim of byte-identical raw output. The manifest now hashes canonical bytes and updated documentation/validator.

The source digest explicitly excludes the whole acceptance-evidence directory, whether untracked, staged or committed. Its 1094-source identity remains unchanged; the validator compares HEAD, index and worktree source bytes, all 204 mappings, actual Go PASS events and command selections, frontend passed cases, every captured gate exit, manifest completeness/hashes and full-file whitespace. No production changes, repeated long race run, relaxed gate, index mutation or history operation is part of this correction. Repair verification passed: artifact validator (204 rows, actual command-selected Go PASS events, 1024 frontend cases, all gate states/hashes, unchanged 1094-source HEAD/index/worktree digest); `git diff HEAD --check` and worktree diff check; full-new-file `git diff --no-index --check /dev/null <path>` over all 103 files in the six artifact paths; generated SQL/TS HEAD/index/worktree drift checks. Four affected captured logs exactly match the documented transform of preserved `.tmp/evidence` originals. Parent must restage the corrected paths and rerun `git diff --cached --check` before committing; independent reviews and platform limitations remain open.

### Integrated closure and final two-axis disposition

Artifact `9435e7b` was committed after the corrected staged whitespace check passed, independently verified from its clean committed tree, and fast-forwarded into `codex/architecture-refinement`. The independent merger approved all 204 mappings, 25 capability records, 117 unique Go witnesses, 32 web cases, 1024 executed frontend tests, source identity, canonicalization and hashes; it found no credential/user-data leak or material merge defect.

Final independent reviews of the entire baseline-to-`9435e7b` integration diff both **APPROVE**: Standards **0 actionable findings**; Spec **0 actionable findings established**. The latter's targeted task/content/proposal/configuration checks and evidence validator passed; neither reviewer reran all long suites, and inspection was not exhaustive across all changed files. See [acceptance.md](../acceptance.md#independent-final-reviews) for separate reports and limitations.

Closed with recorded evidence, not an assertion that 204 stories or all environments are exhaustively certified. Linux/Darwin CGO cross-build attempts remain failed for absent target toolchains; non-Windows runtime, physical devices, live providers and browser storage remain unexecuted. These limitations are retained, not silently waived into PASS. No reproduced predecessor implementation defect remained to route back. All 48 local tickets are now closed; no remote PR or push is part of this closure.
