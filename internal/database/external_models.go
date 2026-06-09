// 业务说明：本文件是业务实现，属于 SQLite 数据访问层，负责把漫画库、系列、阅读进度、任务和元数据状态持久化为稳定数据模型。
// 它连接 sqlc 生成查询与上层领域服务，是资料库筛选、搜索同步和关系图谱的数据基础。
// 维护时应保持 schema、查询定义、事务边界和迁移兼容，避免破坏既有用户数据。

package database

type ExternalLibraryBookRow struct {
	BookID     int64  `json:"book_id"`
	SeriesID   int64  `json:"series_id"`
	SeriesName string `json:"series_name"`
	Path       string `json:"path"`
}
