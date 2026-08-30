// Package external 管理外部库传输会话：把外部库与资料库里的书比对，规划要把哪些书传过去。
//
// 会话只存在于 Manager 的内存中并按 TTL 过期；外部路径须是绝对目录且不与资料库根重叠，
// 否则建会话即被拒。本包遍历外部路径按相对路径与库内书匹配，经窄接口 Store 只读数据库，
// 不写磁盘也不写库，产出覆盖率（SeriesCoverage）与传输计划（TransferPlan），不是传输结果。
//
// 边界：文件复制与暂停闸门归 api（copyFileToExternalLibrary、taskEngine），每条操作落地后由它
// 调用 MarkTransferred 推进会话；本包不定义任务，扫描会话经调用方交来的 TaskHandle 报进度。
// 外部库的文件不入库，资料库的扫描与入库归 scanner。
package external
