// Package api 是 internal 下唯一的入站 HTTP 层：/api（Mihon 端点挂在其下）、OPDS 与 KOReader 同步协议，
// 连同会话与阅读协议的鉴权、SSE 推送、阅读路径上的进程内缓存，以及资料库定时扫描与文件监听的调度。
// 后台任务引擎也归本包：任务表与运行时、活动态与终态、阶段与计数推进、重启函数注册表都收在 taskEngine 上；
// 任务键由发起端点构造后交给它，本包其余代码只经它的方法操作任务表。
//
// 边界：合集、智能书架、作品群与提案裁决的规则在本包；具体活计属于邻居包——scanner 扫描、metadata 刮削、
// parser 读归档、images 转码页图、koreader.Service 匹配指纹、external.Manager 管外部库传输会话、
// database.Store 持久化。暂停闸门与存储令牌是 taskcontrol 与 storageio 的原语；路由器与静态前端由 cmd/server 装配。
package api
