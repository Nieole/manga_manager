---
id: mm-koreader
type: module-design
status: active
title: KOReader 同步与指纹匹配
parent: mm-architecture
---

## Responsibility

实现 kosync 设备账号与同步请求记录的持久化语义、设备文档匹配和进度对账；将命中书目的设备进度翻译为阅读位置输入，阅读事实及投影规则不在本包重做。

## Contract

- 设备注册接收客户端 MD5 密钥，管理端生成随机密钥；认证兼容原密钥的 MD5 与已存客户端哈希，采用常量时间比较，并检查启用状态及绑定站点用户的首登改密要求。自助注册仅在站点单用户时尝试自动绑定，多用户不猜归属。
- `SaveProgress` 认证后校验必填和长度，将百分比夹到 `[0,1]`；按账号、document 保存最后一次推送，服务端接收时间为准，允许倒退。命中与否均可保存协议记录；已找到书不等于位置已应用。
- `NormalizeDocumentForMatch` 按配置选二进制哈希或路径模式。整文件身份是 MD5；路径身份归一分隔符、大小写并取末三级，可忽略扩展名；快速采样指纹与整文件哈希不是同一种身份。
- 命中且页数已知才按百分比换算并夹紧页码，交给 `PositionRecorder.RecordPosition`；按设备绑定用户隔离，归属查询失败不回落全局。新推送传接收时刻，拉取补匹配与 `ReconcileProgress` 重放原记录发生时刻，不制造新的阅读行为。
- `SyncResult` 分开返回协议记录、匹配、位置提交、投影错误和活动软告警。`ResetProgress` 仅删除设备记录并尝试记诊断事件，不等于清除书的阅读事实。
- `ReconcileProgress` 与 `ReconcileProgressWithMatching` 要求非 nil `TaskHandle`，分批过暂停闸门并报计数；只匹配持久化证据、重放设备记录，不读书文件。带匹配配置的入口沿用该运行准入时选定的模式，避免配置修改改变在途对账语义。

## Boundary

不定义 HTTP 路由、后台任务生命周期或阅读事实规则，也不持有内容/路径证据生成与身份补建；共享依赖与组装边见根 `architecture.md`。协议记录与阅读位置是独立写入，不能宣称跨两者原子。

同步诊断事件是尽力写入，不能视为含认证失败、查无记录等所有路径的持久化保证。页数未知时保留设备记录但不推算书页码。

## Evidence

- `service.go`、`credentials.go`：账号、凭据、协议存储、文档匹配、阅读委托与对账端口。
- `service_test.go`、`fingerprint_extra_test.go`、`progress_clamp_extra_test.go`：认证、注册归属、最后写入胜出与匹配归一。
- `position_test.go`：原时刻重放、用户隔离、清除后不复活旧位置、协议保存与位置提交独立、未知页数及活动软告警。
- `rebuild_identities_disk_work_test.go` 经 `bookidentity.Service.Rebuild` 验证证据补建的磁盘资格，不代表 KOReader 持有重建实现；实现归属另见根架构及 `bookidentity` 的 `evidence_test.go`、`rebuild_test.go`。
