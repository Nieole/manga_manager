// Package runtimecfg 把配置派生的进程级资源重建收拢为单一入口 Apply。
//
// 归它管：按给定配置重设归档句柄池（parser.InitPool）、AI 放大并发（images.InitProcessor）与
// 日志级别（logger.SetLevel）；启动、配置文件热重载、经 API 保存都走同一个 Apply，副作用因而一致。
//
// 不归它管：配置的加载、校验与落盘属于 config，文件监听与防抖属于 config.Watcher（以回调接过 Apply），
// 各资源自身的构造与语义属于 parser、images、logger。本包不持有状态，也不决定何时调用 Apply。
package runtimecfg
