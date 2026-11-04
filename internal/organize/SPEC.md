---
id: mm-organize
type: module-design
status: active
title: 藏书编排与生成建议裁决
parent: mm-architecture
---

## 责任与契约

拥有合集成员、智能书架条件、系列关系与推导作品群、AI 分组建议裁决，以及有序阅读列表。人工成员、过滤推导成员和关系连通分量是不同规则，不提供一个任意改写全部对象的通用成员接口。

阅读列表独立于合集，保存系列成员顺序、备注并投影当前用户的继续阅读；排序请求以列表内条目标识操作，须完整验证后写入，不能借外部条目重排别的列表。

AI 分组建议修改合集编排，不修改系列元数据。应用建议需复核相关系列和当前状态；不能因为生成时存在就跳过应用时的冲突检测。关系变动后的作品群重建属于该用例，不由页面拼接 SQL 维护。

## 边界

公开服务持有校验与事务，授权和协议错误映射留给适配器。生成建议的后台运行配方在应用层，本包接受候选并负责裁决，不拥有调度、HTTP 或服务生命周期。与兄弟模块的实际依赖归 `mm-architecture`。

核对入口：`ReadingLists`、`Relations`、`franchiseRebuilder`、`collections_test.go`、`smart_collections_test.go`、`reading_lists_test.go`、`ai_grouping_test.go`。
