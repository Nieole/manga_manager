// Manga Manager Service Worker - 静态资产与离线阅读缓存
const CACHE_NAME = 'manga-manager-v2';
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

function offlineFallback(request) {
    return caches.open(OFFLINE_BOOK_CACHE)
        .then(cache => cache.match(request))
        .then(response => response || caches.match(request));
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
