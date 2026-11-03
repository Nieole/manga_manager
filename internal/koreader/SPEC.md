---
id: mm-koreader
type: module-design
status: draft
title: KOReader 同步与指纹匹配
parent: mm-architecture
---

## Responsibility

实现 kosync 设备账号与同步请求记录的持久化语义、文档指纹匹配和身份补建；将命中书目的设备进度翻译为阅读位置输入，阅读事实及投影规则不在本包重做。

## Contract

- 设备注册接收客户端 MD5 密钥，管理端生成随机密钥；认证兼容原密钥的 MD5 与已存客户端哈希，采用常量时间比较，并检查启用状态及绑定站点用户的首登改密要求。自助注册仅在站点单用户时尝试自动绑定，多用户不猜归属。
- `SaveProgress` 认证后校验必填和长度，将百分比夹到 `[0,1]`；按账号、document 保存最后一次推送，服务端接收时间为准，允许倒退。命中与否均可保存协议记录；已找到书不等于位置已应用。
- `NormalizeDocumentForMatch` 按配置选二进制哈希或路径模式。整文件身份是 MD5；路径身份归一分隔符、大小写并取末三级，可忽略扩展名；快速采样指纹与整文件哈希不是同一种身份。
- 命中且页数已知才按百分比换算并夹紧页码，交给 `PositionRecorder.RecordPosition`；按设备绑定用户隔离，归属查询失败不回落全局。新推送传接收时刻，拉取补匹配与 `ReconcileProgress` 重放原记录发生时刻，不制造新的阅读行为。
- `SyncResult` 分开返回协议记录、匹配、位置提交、投影错误和活动软告警。`ResetProgress` 仅删除设备记录并尝试记诊断事件，不等于清除书的阅读事实。
- 重建与对账要求非 nil `TaskHandle`，分批过暂停闸门并报计数；整文件重建经 `Disk` 读取，路径重建与对账不读书文件。坏文件跳过且推进游标，磁盘入口拒绝则中止。

## Boundary

未确认：以下边界依据当前实现整理，待用户审阅。

不定义 HTTP 路由、后台任务生命周期或阅读事实规则；协议记录与阅读位置是独立写入，不能宣称跨两者原子。共享依赖与组装边见根 `architecture.md`。

实现观察：同步事件是尽力写入，失败被忽略；`doc.go` 的“每次推送、拉取都记一条”不能视为含认证失败、查无记录等路径的持久化保证。页数未知时保留设备记录但不推算书页码。`doc.go` 将任务定义与生命周期归 API、称整文件读取均经 TaskHandle，也有归属与模式漂移；当前路径模式重建不读书文件，应用层持有任务编排。

## Evidence

- `service.go`、`fingerprint.go`：账号、协议存储、匹配、阅读委托与批处理端口。
- `service_test.go`、`fingerprint_extra_test.go`、`progress_clamp_extra_test.go`：认证、注册归属、最后写入胜出与匹配归一。
- `position_test.go`：原时刻重放、用户隔离、清除后不复活旧位置、协议保存与位置提交独立、未知页数及活动软告警。
- `rebuild_identities_disk_work_test.go`：磁盘入口、暂停、拒绝中止和坏文件游标。
