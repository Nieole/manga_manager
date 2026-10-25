// Package external 管理外部库会话、传输计划与单文件原子复制规则。
//
// 会话只存在于 Manager 的内存中并按 TTL 过期；外部路径须是绝对目录且不与资料库根重叠，
// 否则建会话即被拒。本包遍历外部路径按相对路径与库内书匹配，经窄接口 Store 只读数据库，
// 不写库，产出覆盖率（SeriesCoverage）与传输计划（TransferPlan）。CopyFile 保留已存在目标，
// 通过同目录临时文件落盘并原子提交新目标，避免暴露未完成的文件。
//
// 边界：application/diskjobs 发起完整后台用例，每条操作落地后调用 MarkTransferred 推进会话；
// 本包不定义任务，扫描通过上下文响应暂停取消，经调用方交来的 TaskHandle 报进度。
// 外部库的文件不入库，资料库的扫描与入库归 scanner。
package external
