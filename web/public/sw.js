// GeoTracker service worker: makes the app installable and opens it offline.
// Only the app shell is cached. API responses (location data) are never cached.
const CACHE = "gt-shell-v2"; // bumped so the fixed worker replaces cached state

self.addEventListener("install", () => self.skipWaiting());

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  );
});

self.addEventListener("fetch", (event) => {
  const req = event.request;
  const url = new URL(req.url);
  if (req.method !== "GET" || url.origin !== self.location.origin) return;
  if (url.pathname.startsWith("/api/") || url.pathname.startsWith("/ingest/")) return;

  // Hashed build assets never change: cache first.
  if (url.pathname.startsWith("/assets/")) {
    event.respondWith(
      caches.match(req).then((hit) => hit || fetch(req).then((res) => {
        // Clone now: by the time the cache opens, the page has already consumed the body.
        if (res.ok) { const copy = res.clone(); event.waitUntil(caches.open(CACHE).then((c) => c.put(req, copy))); }
        return res;
      })),
    );
    return;
  }

  // Pages: network first so updates arrive immediately; the cached shell when offline.
  if (req.mode === "navigate") {
    event.respondWith(
      fetch(req).then((res) => {
        if (res.ok) { const copy = res.clone(); event.waitUntil(caches.open(CACHE).then((c) => c.put("/", copy))); }
        return res;
      }).catch(() => caches.match("/")),
    );
  }
});
