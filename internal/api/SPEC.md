---
id: mm-api
type: module-design
status: active
title: HTTP 与客户端协议适配
parent: mm-architecture
references: [mm-adr-0003, mm-adr-0007]
---

## 责任与入口

把 HTTP 与客户端协议转换成应用调用：解析路径和请求、会话与管理员授权、CSRF、响应及错误映射。Web API、OPDS、Mihon、KOReader 保留各自认证和协议兼容语义，不因共用进程而合成一个授权入口。

只接受经过授权的用户身份；阅读写入显式传递该用户，不能从请求任意指定另一个人。用户名契约归 `mm-adr-0003`，任务寻址归 `mm-adr-0007`。

## 边界

控制器可直接使用藏书查询、协议投影与缓存适配，不在此重建阅读、编排、提案或迁移事务。后台工作的声明与重启配方归应用能力；这里映射任务控制、历史和 SSE 快照，不持有另一套准入。

`NewServer` 连接 HTTP 叶效果和运行时；底层服务不回调 HTTP 请求处理器。响应契约与前端生成类型须一致，协议差异在各适配器内消化。

核对入口：`controller.go`、`auth_username_case_test.go`、`collection_authorization_test.go` 与协议控制器测试。
