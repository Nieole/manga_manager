// Package booksort 解析书名与卷标签里的卷话序号（阿拉伯数字、中文数字、西文前缀），据此定自然序。
//
// 它是一组无状态纯函数：输入只有标签文本与 database.Book 的字段，不碰磁盘也不读写数据库。
// 书的有效序号优先从 Name、Title、Number 的文本现取，落库的 sort_number 只作最后兜底。
//
// 边界：把序号写进 sort_number 归 scanner；何时用 CompareBooks、CompareLabels 重排结果集
// 由 api 的控制器决定；从内容或外部源推导元数据、产出提案不属于这里。
package booksort
