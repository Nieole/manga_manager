// 业务说明：本文件是浏览器 Service Worker，负责静态资产缓存和阅读器离线缓存的兜底读取。
// 它让用户在断网或后端短暂不可用时仍能打开已缓存书籍、书籍信息和阅读页面。
// 维护时应严格区分阅读相关 GET 请求与普通 API，避免把任务、设置、SSE 等实时接口缓存成过期业务状态。
// Manga Manager Service Worker - 静态资产与离线阅读缓存
const CACHE_NAME = 'manga-manager-v3';
const OFFLINE_BOOK_CACHE = 'manga-manager-offline-books-v1';
const STATIC_ASSETS = [
    '/',
    '/offline',
    '/manifest.json',
];

// 安装时预缓存核心资源
self.addEventListener('install', (event) => {
    event.waitUntil(
        caches.open(CACHE_NAME).then(cache => cache.addAll(STATIC_ASSETS))
    );
    self.skipWaiting();
});

// 激活时清理旧版缓存
self.addEventListener('activate', (event) => {
    event.waitUntil(
        caches.keys().then(keys =>
            Promise.all(keys.filter(k => ![CACHE_NAME, OFFLINE_BOOK_CACHE].includes(k)).map(k => caches.delete(k)))
        )
    );
    self.clients.claim();
});

function isReaderOfflineRequest(url) {
    return url.pathname.startsWith('/api/pages/')
        || url.pathname.startsWith('/api/book-info/')
        || url.pathname.startsWith('/reader/');
}

async function offlineFallback(request) {
    const bookCache = await caches.open(OFFLINE_BOOK_CACHE);
    // 先按完整 URL（含 query）精确匹配。
    let hit = await bookCache.match(request);
    if (hit) return hit;
    // 阅读页图路径已含页号、是内容寻址的，但 URL 带画质/格式/滤镜等查询参数；用户离线下载后若改过阅读
    // 偏好，读取时用「当前偏好」重建的 URL 与缓存 URL 的 query 不一致，精确匹配会失败、离线读图静默失败。
    // 对 /api/pages/ 请求放宽为忽略查询参数，按页路径命中已缓存字节，从而与画质/格式设置解耦。
    const url = new URL(request.url);
    const isPage = url.pathname.startsWith('/api/pages/');
    if (isPage) {
        hit = await bookCache.match(request, { ignoreSearch: true });
        if (hit) return hit;
    }
    // 再回退到静态资源缓存（同样先精确，页图再忽略查询）。
    hit = await caches.match(request);
    if (hit) return hit;
    if (isPage) {
        hit = await caches.match(request, { ignoreSearch: true });
        if (hit) return hit;
    }
    return undefined;
}

// 网络优先策略：先尝试网络，如断网则降级到缓存
self.addEventListener('fetch', (event) => {
    const { request } = event;
    if (request.method !== 'GET') return;

    const url = new URL(request.url);
    if (url.origin !== self.location.origin) return;

    // SSE 与非阅读 API 不走缓存。阅读页和书籍信息允许离线缓存回退。
    if (url.pathname.startsWith('/api/events')) return;
    if (url.pathname.startsWith('/api/') && !isReaderOfflineRequest(url)) return;

    if (isReaderOfflineRequest(url)) {
        event.respondWith(
            fetch(request).catch(() => offlineFallback(request))
        );
        return;
    }

    // 静态资产：网络优先 + 缓存后备
    event.respondWith(
        fetch(request)
            .then(response => {
                // 如果响应有效，而且是以 http/https 开头的请求才存入缓存 (排除 chrome-extension 等)
                if (response.ok && request.method === 'GET' && request.url.startsWith('http')) {
                    const clone = response.clone();
                    caches.open(CACHE_NAME).then(cache => cache.put(request, clone));
                }
                return response;
            })
            .catch(() => caches.match(request))
    );
});
