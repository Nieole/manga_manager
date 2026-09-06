// Package api 是 internal 下唯一的入站 HTTP 层：/api（Mihon 端点挂在其下）、OPDS 与 KOReader 同步协议，
// 连同会话与阅读协议的鉴权、SSE 推送、阅读路径上的进程内缓存，以及资料库定时扫描与文件监听的调度。
// 后台任务的**启动入口**与**重启函数**注册表归本包：taskEngine 把它们接到 internal/task 的领域引擎与
// internal/taskstore 的落盘上，状态机、终态裁决与准入都在那两包，本包不留任务表。任务体只收下引擎
// 交来的**运行句柄**（taskrun.Handle），不够到 Controller。
//
// 边界：合集、智能书架、作品群与提案裁决的规则在本包；具体活计属于邻居包——scanner 扫描、metadata 刮削、
// parser 读归档、images 转码页图、koreader.Service 匹配指纹、external.Manager 管外部库传输会话、
// database.Store 持久化。限流规则与闸门实现属于 diskwork、storageio 与 taskcontrol；路由器与静态前端由 cmd/server 装配。
package api
