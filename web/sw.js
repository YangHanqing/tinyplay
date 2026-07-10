const CACHE_NAME = 'tinyplay-shell-v20260710-icons';
const SHELL_ASSETS = [
  '/',
  '/index.html',
  '/manifest.webmanifest',
  '/static/app.js',
  '/static/i18n.js',
  '/static/styles.css',
  '/static/pwa-icon-192.png',
  '/static/pwa-icon-512.png',
  '/static/pwa-maskable-192.png',
  '/static/pwa-maskable-512.png',
  '/static/apple-touch-icon.png',
];

self.addEventListener('install', event => {
  event.waitUntil(
    caches.open(CACHE_NAME)
      .then(cache => cache.addAll(SHELL_ASSETS))
      .then(() => self.skipWaiting())
  );
});

self.addEventListener('activate', event => {
  event.waitUntil(
    caches.keys()
      .then(keys => Promise.all(keys.filter(key => key !== CACHE_NAME).map(key => caches.delete(key))))
      .then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', event => {
  const { request } = event;
  const url = new URL(request.url);
  if (url.origin !== self.location.origin) return;

  if (url.pathname.startsWith('/api/')) {
    event.respondWith(fetch(request).catch(() => offlineAPIResponse()));
    return;
  }

  if (request.method !== 'GET') return;

  if (request.mode === 'navigate') {
    event.respondWith(
      fetch(request)
        .then(response => cacheAndReturn(request, response))
        .catch(() => caches.match('/index.html').then(cached => cached || Response.error()))
    );
    return;
  }

  if (isShellAsset(url.pathname)) {
    event.respondWith(
      caches.match(request)
        .then(cached => cached || fetch(request).then(response => cacheAndReturn(request, response)))
        .catch(() => Response.error())
    );
  }
});

function isShellAsset(pathname) {
  return SHELL_ASSETS.includes(pathname) || pathname === '/sw.js';
}

function cacheAndReturn(request, response) {
  if (!response || !response.ok) return response;
  const copy = response.clone();
  caches.open(CACHE_NAME).then(cache => cache.put(request, copy));
  return response;
}

function offlineAPIResponse() {
  return new Response(JSON.stringify({ detail: 'TV service is offline' }), {
    status: 503,
    headers: {
      'Content-Type': 'application/json; charset=utf-8',
      'X-TVRemote-Offline': '1',
      'Cache-Control': 'no-store',
    },
  });
}
