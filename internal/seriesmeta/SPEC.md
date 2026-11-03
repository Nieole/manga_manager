---
id: mm-seriesmeta
type: module-design
status: draft
title: 来源区分的系列元数据写入
parent: mm-architecture
---

## Responsibility

集中系列元数据的来源特定写入策略、内容版本计算与 ComicInfo 导出投影；不统一抹平手工、提案、扫描和批量编辑的不同关联语义。

## Contract

- 查询接口接收调用方的事务句柄；本包不开事务。调用方负责失败回滚及派生数据刷新编排；`ApplyProposal` 内的沿革和统计刷新也在该事务中执行。
- `SaveManual` 替换标量，可修改锁及锁住的字段；标签、作者、链接为 nil 时保留，显式空集合清空。可选 `ExpectedVersion` 与当前内容指纹比较，冲突返回 `ErrConflict` 和当前版本；不擅自清洗标量原文。
- `ApplyProposal` 接受已裁决字段和系列快照，非空标量覆盖未锁字段；仅 `WrittenFields` 选中的未锁标签、作者整体清空重建。写入沿革，关联失败上抛；Bangumi 来源且条目 ID 有效时，缺少同名链接才追加来源链接。
- `PrepareScanInitial` 用目录名作标题并默认锁标题；`PrepareScanUpdate` 使用调用方缓存的行与锁，保留已有标题、为空且未锁时回落目录名，其余未锁标量可被空值清除。`AppendScanAssociations` 只追加标签、作者，不做替换。
- `EditSelection` 修改非 nil 的状态、出版商，标签先增后删，不改锁和沿革；错误要求整个选择回滚。`Version` 对可编辑内容求指纹，集合去空白、去重排序，不纳入身份、阅读状态和统计；`BuildComicInfoForBook` 只投影已知事实。

## Boundary

未确认：以下边界依据当前实现整理，待用户审阅。

不持有提案入队、状态推进或裁决事务，不创建系列身份、书目和封面，不执行归档写回。跨模块协作归根 `architecture.md`。

实现观察：扫描关联忽略集合锁和单次写入错误，扫描标量不从 ComicInfo 提取连载状态；这是当前来源差异，不是所有写入器共享的保证。

## Evidence

- `manual.go`、`proposal.go`、`scan.go`、`scan_associations.go`、`bulk.go`、`version.go`、`comicinfo.go`：各入口与事务能力范围。
- `manual_test.go`、`proposal_test.go`、`proposal_compat_test.go`：版本冲突、回滚、未选与显式空集合、锁及来源沿革。
- `scan_test.go`、`bulk_test.go`：扫描锁矩阵与追加容错、批量先增后删及跨分块整体回滚。
