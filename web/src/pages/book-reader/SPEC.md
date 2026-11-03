---
id: mm-web-reader
type: submodule-design
status: draft
title: 浏览器阅读与本机离线
parent: mm-web
---

## 责任与生命周期

未确认：以下契约依据当前阅读 hooks 和离线测试整理，待用户审阅。

拥有当前书的数据与页图、翻页/瀑布流导航、阅读偏好、书签、进度与时长上报。`BookReader` 组合此目录能力；当前书的响应和报告必须绑定书标识，切书后的迟到回调不能写入新书视图或把旧页当作新书进度。

页图预读缓存服务连续阅读；下载到本机的离线藏书与待回传进度是独立持久状态，不随当前书卸载一起释放。下载收尾必须仍持有该书下载令牌和用户归属代次，删除书、替换下载或换用户后不得复活旧缓存索引。

离线进度保留事实时间与标识，在线后批量同步，必要时使用兼容回退；提交需带认证和 CSRF。归属变更时清理上一用户的数据，并防止在途下载和同步跨用户落地。缓存仅是本机能力，不承诺浏览器永不回收。

## 边界

本模块不拥有全站 SSE 或后台任务状态，也不重写后端阅读事实的时序规则。交互页码与服务端进度转换统一经导航/进度 helpers；设备偏好与离线数据不等于服务器会话。

核对入口：`useReaderBookData`、`useReaderProgressPipeline`、`offlineReader`、`readerSwitchLateReport.test.tsx`、`offlineOwner.test.ts`、`offlineDownload.test.ts`、`offlineDelete.test.ts`。
