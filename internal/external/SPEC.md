---
id: mm-external
type: module-design
status: active
title: 外部目录传输会话
parent: mm-architecture
---

## Responsibility

管理外部目录的短期传输会话、书目覆盖率、缺书传输计划与单文件暂存复制。外部目录只是传输目标及对账输入，永不成为本包维护的扫描目录或藏书目录。

## Contract

- `CreateSession` 要求已存在的绝对目录，拒绝无卷文件系统根及与任一已登记资料库根相等或互相包含的路径；同时比较字面路径与解析物理边界后的路径，已登记根暂时不存在时仍解析其现有祖先，无法安全解析则拒绝。会话仅存于 `Manager` 内存，按 `UpdatedAt` 与 TTL 在访问时清理，读取、规划及更新按资料库作用域校验。
- `ScanSession` 读取本库书目，遍历受支持归档，以相对路径键匹配，可忽略扩展名，输出覆盖率和扫描计数；完成变 ready，失败变 failed。遍历和匹配循环经上下文暂停/取消，非 nil `TaskHandle` 仅提供 `Advance`。
- `PrepareTransfer` 只接受 ready 会话，去重系列 ID，一次批量读取书目并排除其他库，产生已有/缺少计数及明确源、目标路径的操作；规划本身不复制。调用方落地后用 `MarkTransferred` 按匹配键幂等推进会话，可清除会话或使库会话失效。
- `CopyFile` 返回是否跳过已有目标；复制到同目录临时文件，Sync、关闭并保留源 mtime 后 rename 提交，正常失败路径清理暂存文件。最终目标在提交前不暴露半成品；复制前与提交前均检查目标是否已存在。

## Boundary

`Store` 只有只读查询，没有写库或事务能力；外部文件不入库，不更新藏书内容或阅读事实。本包不持有完整传输任务、磁盘调度及任务生命周期；复制后确认会话由调用方负责。共享协作边见根 `architecture.md`。

`ValidateSession` 在使用保留会话或计划时复核全部已登记资料库、所属库路径及会话物理边界；不安全或已变化的会话失效，不静默重规划。`ValidateOperation` 进一步要求目标仍在会话外部根下，并重新检查目标与全部资料库的重叠，不能因创建会话时通过就跳过使用时校验。

复制的末次 Stat 与 rename 之间不是无覆盖原子条件提交，不能扩大成任意并发竞争下的绝不覆盖保证。会话不跨进程重启持久化。

## Evidence

- `manager.go`、`copy.go`：会话状态、TTL、路径键、窄只读接口、批量计划与暂存提交。
- `manager_test.go`、`manager_path_test.go`、`prepare_transfer_test.go`：过期、资料库隔离、路径拒绝、解链与批量查询/去重。
- `boundary_test.go`、`transfer_test.go` 的 `TestTransferRechecksRegisteredRootsBeforePublishingCurrentBook`：全部已登记资料库的边界拒绝、根变更后的会话失效及传输发布前复核。
- `copy_test.go`、`copy_atomicity_test.go`（Unix）：已有目标跳过、复制内容与 mtime、复制途中不暴露最终文件及暂存清理。
