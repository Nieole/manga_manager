// Package database 是 SQLite 持久层：Store 接口把 sqlc 生成的查询与手写跨表事务合成一个数据出入口，Migrate 负责建表、
// 幂等 DDL 与按 user_version 门控的一次性回填；本包不 import 任何兄弟包。藏书、用户与任务的全部落库形状都归它——
// 资料库/系列/书、标签与作者、合集与智能书架、系列关系、提案、账号与会话、每用户阅读进度与统计、阅读列表与书签、
// KOReader 账号与进度；读回来的方式也归它：FTS5 与 2-gram 搜索索引、子串回退、分页游标与聚合统计。
//
// 不归它的是磁盘与网络（自己的 SQLite 库文件除外）：扫描在 scanner、图片在 images、刮削与 AI 在 metadata、
// 外部库传输会话在 external、指纹在 koreader；任务的活动态归 api 的 taskEngine，暂停闸门归 taskcontrol，
// 存储令牌归 storageio，作品群的连通分量由 api 的 RebuildFranchiseCollections 算好后回写。
package database
