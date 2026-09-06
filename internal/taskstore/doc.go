// Package taskstore 是 task.Store 的 SQLite 适配器：表结构、迁移、查询与聚合都归它。
//
// **准入由数据库保证**：runs 上两条部分唯一索引分别兜住「同一任务只有一次**活动**运行」与
// 「最多一条**排队中**的运行」，违例被翻成 task.ErrRunAlreadyActive 与 task.ErrRunAlreadyQueued。
// 判据只此一处，内存里不得再存第二份。
//
// 两条索引的状态集合由 task.ActiveStatuses 拼出而不是手抄，因此领域加一个活动态时索引跟着走。
// 侧表随运行级联删除，这要求调用方的连接开着 foreign_keys——Migrate 为此拦一道。
package taskstore
