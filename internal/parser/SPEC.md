---
id: mm-parser
type: module-design
status: draft
title: 归档、页面与内嵌元数据解析
parent: mm-architecture
---

## 职责

按文件路径解析 zip/cbz、rar/cbr 归档，筛出图片页并确定归档内页序，读取原始条目字节，解析与合并 ComicInfo.xml；管理共享归档句柄池与 ZIP 元数据写回。

## 公开契约

- `OpenArchive` 按扩展名分发，`Archive` 提供页面列表、按条目名读取、元数据读取与关闭。页序使用封面关键字、浅层目录优先及自然数排序，是启发式而非漫画语义识别。
- 条目读取经 `readEntryLimited` 检查声明大小与实际读取上限，不直接信任归档头；ComicInfo 解析容忍部分脏数字、BOM 与字符集声明，非法 XML 返回错误。
- `WriteComicInfoIntoArchive` 只支持 zip/cbz，rar/cbr 返回 `ErrArchiveNotWritable`。已有 XML 合并保留未建模字段等内容，危险原文拒写；通过临时归档替换原文件，不承诺任意归档格式可写。
- `GetArchiveFromPool` 返回共享句柄与归还函数，借用方必须归还，不能自行关闭共享句柄。池按修改时间与大小判断文件变化；淘汰立即移出池，引用归零才关闭。`InitPool` 可调整容量，`CloseArchivePool` 用于所有者退出后的收尾。

## 边界与限制

未确认：以下边界依据当前实现整理，待用户审阅。

只识别路径与归档内条目，不认识书、系列、资料库或数据库；不遍历资料库、不处理页图缩放与 HTTP，也不负责卷 IO 许可，调用方须在适当入口申领。无兄弟包导入，归档内页序不得替代系列内卷话排序。

## 证据

`Archive`、`OpenArchive`、`readEntryLimited`、`MergeComicInfoXML`、`ArchivePool`；`parser_test.go` 的 `TestReadEntryLimited`、`TestPoolEvictionDoesNotCloseBorrowedHandle`，以及 `comicinfo_merge_test.go`、`comicinfo_writeback_test.go`、`rar_test.go`。
