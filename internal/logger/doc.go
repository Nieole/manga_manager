// Package logger 是进程级日志基础设施：接管 slog 的默认 logger，把结构化日志写到 stdout，并在 Init 传入
// 目录时同时写入该目录下的 manga_manager.log（自动滚动、压缩、限量保留）；标准库 log 的输出也并入同一路。
//
// 边界：本包只负责写日志与告知写到哪里——Init 定输出目的地与格式，SetLevel 在运行期改级别并拒绝无法识别
// 的值，LogFilePath 返回实际写入路径（未启用文件日志时为空串）。级别值来自 config 的 Logging.Level，启动
// 时传给 Init、其后经 runtimecfg.Apply 转发；日志的读取、解析与对外呈现属于 api 的 getSystemLogs。
// 本包不导入兄弟包，也不承载资料库、系列、任务等领域概念。
package logger
