---
id: mm-web
type: module-design
status: active
title: 浏览器应用与客户端状态归属
parent: mm-architecture
---

## 责任与内部边界

浏览器应用负责藏书浏览、编排、阅读与管理交互。身份与角色由 `auth` 持有，`App` 配置路由；`session` 协调全站连接和失效；功能 hooks 持有查询、功能投影及补拉；页面拥有筛选、选择、展开、弹窗与待提交草稿。页面卸载不能销毁应持续存在的全站连接，连接重建也不能成为清空页面编辑态的理由。

Layout 路由位于 `ApplicationSessionScope` 内；全屏阅读器与作品群图不在该作用域，不能要求所有页面依赖会话 Provider。`features/task-observation` 共享任务快照与气泡投影，不由每个页面各建 SSE；`features/external-library` 管传输会话功能状态。本机阅读数据的生命周期归 `mm-web-reader`。

## 服务边界

API 是规则与授权的最终裁决者；前端守卫只避免向无权限用户呈现无法使用的整屏管理操作。`src/api/generated.ts` 是生成的服务契约，不手工维护平行类型。功能异步请求必须校验所请求的作用域仍有效，避免切库或切系列后旧结果覆盖新视图。

locale 与 theme 提供表现偏好，不混入业务事实或用户在线会话。验证命令、依赖说明归 `AGENTS.md` 和 `web/README.md`，不在此复制。

核对入口：`src/App.tsx`、`src/session/ApplicationRoutes.test.tsx`、任务观察 hooks 与页面切换竞态测试。
