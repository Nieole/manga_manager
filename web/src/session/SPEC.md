---
id: mm-session
type: submodule-design
status: active
title: 浏览器会话推送与刷新协调
parent: mm-web
---

## 责任与契约

一个身份/角色作用域内建立一条 `/api/events` SSE 连接，解码推送、跟踪投递链并广播刷新水位、运行帧和恢复通知。身份或角色改变时，通过 keyed 实例释放旧连接、监听集合和投影；不触碰设备偏好或离线数据归属。

普通刷新失效藏书查询；管理员的运行推送才进入运行订阅。重连或投递链缺口通知消费者补拉，不能从序号数字是否连续推断是否漏帧。功能消费者使用 `useSessionRunPush`、`useSessionRecovery`，不再解析 SSE 或另开连接。

## 边界

这里只提供连接和水位，不缓存目录查询、不持有页面筛选草稿、不决定任务列表展示。`useLibraryRefresh` 根据区域、库或系列选择水位；失效发出不等于查询已成功恢复，恢复请求的竞态与错误归消费者处理。

兄弟功能间不通过会话对象共享任意可变结果。作用域外的全屏路由不为获取一个无关水位而强行接入全站连接。

核对入口：`ApplicationSessionProvider`、`useEventStream`、`ApplicationSession.test.tsx`、`catalogConsumers.test.tsx`。
