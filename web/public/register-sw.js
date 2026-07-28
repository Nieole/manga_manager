// 这段 SW 注册脚本必须是**独立文件**而不是 index.html 里的内联 <script>：
// 后端 securityHeaders 下发的 CSP 是 script-src 'self'，内联脚本会被静默拦掉，
// SW 永不注册、sw.js 的离线兜底与 PWA 安装随之全部失效。
//
// 同样不能挪进 src/main.tsx：它存在的意义正是「主 bundle 因旧壳 404 而挂掉时仍能换上新 SW」，
// 一旦被打包进 hash 化 chunk 就失去了这层保险。
//
// 也必须放在 web/public/ 下：该目录的文件被 vite 原样复制到 dist 根、不做 hash 重写，
// index.html 里的 <script src="/register-sw.js"> 因此能保持这个稳定 URL。

// 注册 Service Worker，并在「新版本 SW 接管当前页」时自动刷新一次，避免部署后仍运行旧壳——
// 旧的 hash 化 chunk 可能已随新版本删除，继续跑旧壳会在路由懒加载时 404 白屏。
// 仅当页面「一开始就已被某个 SW 控制」时才在 controllerchange 后重载：首次安装（此前无 controller）
// 不重载以免初次加载被打断；refreshing 标志防止重载循环。sw.js 内已 skipWaiting + clients.claim，
// 故新版本会立即接管并触发 controllerchange。
if ('serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    const controlledAtStart = !!navigator.serviceWorker.controller;
    let refreshing = false;
    navigator.serviceWorker.addEventListener('controllerchange', () => {
      if (!controlledAtStart || refreshing) return;
      refreshing = true;
      window.location.reload();
    });
    navigator.serviceWorker.register('/sw.js').catch(() => {});
  });
}
