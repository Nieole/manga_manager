---
id: mm-metadata
type: module-design
status: active
title: 元数据源与候选生成
parent: mm-architecture
---

## Responsibility

将外部漫画数据源与 LLM 响应转换为内存候选 `SeriesMetadata`，并生成推荐、合集分组建议与测试响应；不把候选当成已接受的系列事实。

## Contract

- `Provider` 提供按标题获取与分页搜索；`AIProvider` 扩展推荐、分组和 `TestLLM`。结果携带来源、置信度及封面 URL，分页与无结果口径由各适配器实现。
- `WithLocale` 决定提示词语言：英文前缀归 `en-US`，其余及缺省归 `zh-CN`。源侧状态只报告已知事实，缺失、无法识别及源侧 `unknown` 留空；写入端专用的 `StatusCodeOrUnknown` 不用于候选出口。
- 五个公共数据源的真实 HTTP 请求共享进程级并发闸门；重试识别 `Retry-After`，退避和等令牌可取消，POST 重试重建请求体。上游错误处理脱敏密钥、截断响应体，不能将完整错误页直接写日志。

## Boundary

不写数据库、不落盘、不下载封面，也不拥有提案生命周期、裁决或任务调度。共享依赖归属见根 `architecture.md`。`doc.go` 仍将提案裁决和刮削调度归 API，存在归属漂移；本规格按当前实现区分候选生成、提案裁决与应用编排。

实现观察：并发闸门只覆盖表中数据源，未列出的 LLM 不受此闸门限制；它不是每小时配额保证。

## Evidence

- `provider.go`、`locale.go`、`provider_budget.go`、`errors.go`：公开结果、状态出口、请求预算及错误处理。
- `llm_status_test.go`、`prompts_locale_test.go`、`provider_routing_test.go`：状态缺省、语言与 LLM 路由。
- `provider_budget_test.go`、`retry_loop_test.go`、`errors_test.go`：跨实例并发、重试取消、请求体重建与脱敏截断。
