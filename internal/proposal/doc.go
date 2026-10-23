// Package proposal 管理元数据提案的入队、裁决与读取，按当前字段锁过滤，并按待裁决提案
// 与最近拒绝记录逐字去重。应用和读出均重新判定当前锁。
// QueueStatus 与 ApplyStatus 表达业务结局，error 只表示故障；前置检查与事务内 CAS
// 发现的抢先均归为 ApplyConflict。提案生命周期及裁决事务由本包持有。
// HTTP 编解码、字段标签与提示文案归 internal/api，SQL 归 internal/database，
// 已裁决的系列字段写入归 internal/seriesmeta。本包只认字段名。
// AI 分组的合集成员建议不属于提案。
package proposal
