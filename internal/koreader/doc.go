// Package koreader 实现 kosync 同步协议的服务端语义：KOReader 设备账号的注册、认证与生命周期管理，
// 阅读进度的接收、查询与管理端重置（每次推送、拉取都记一条同步事件），以及从书的内容或路径导出指纹。
// 进度到达后按配置的匹配模式归一化 document 去找书，命中就投影为该书的页码进度与阅读活动；
// 未命中的留作未匹配，之后由 ReconcileProgress 或下一次拉取补上；RebuildBookIdentities 直接读书文件算指纹，回填缺身份的书。
//
// 边界：HTTP 路由与编解码、任务的定义与生命周期属于 internal/api，SQL 属于 internal/database，
// 匹配模式读自 internal/config，扫描时给书写指纹的是 internal/scanner（它调用本包的指纹函数）；
// 本包不定义任务：RebuildBookIdentities、ReconcileProgress 的批循环过**暂停闸门**、发起
// **磁盘作业**（前者逐本读整个文件）都经调用方交来的 TaskHandle，本包不自己持有这两样能力。
package koreader
