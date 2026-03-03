// Manga Manager Service Worker - 静态资产缓存
const CACHE_NAME = 'manga-manager-v1';
const STATIC_ASSETS = [
    '/',
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
            Promise.all(keys.filter(k => k !== CACHE_NAME).map(k => caches.delete(k)))
        )
    );
    self.clients.claim();
});

// 网络优先策略：先尝试网络，如断网则降级到缓存
self.addEventListener('fetch', (event) => {
    const { request } = event;

    // API 请求和 SSE 不走缓存
    if (request.url.includes('/api/')) return;

    // 静态资产：网络优先 + 缓存后备
    event.respondWith(
        fetch(request)
            .then(response => {
                // 如果响应有效，将其存入缓存
                if (response.ok && request.method === 'GET') {
                    const clone = response.clone();
                    caches.open(CACHE_NAME).then(cache => cache.put(request, clone));
                }
                return response;
            })
            .catch(() => caches.match(request))
    );
});
