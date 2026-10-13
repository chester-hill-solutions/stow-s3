const cacheName = "stow-browser-editor-v1";
self.addEventListener("install", (event) => {
  event.waitUntil((async () => {
    const assets = await (await fetch("/assets.json")).json();
    await (await caches.open(cacheName)).addAll(assets);
    await self.skipWaiting();
  })());
});
self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});
self.addEventListener("fetch", (event) => {
  const url = new URL(event.request.url);
  if (event.request.method !== "GET" || url.origin !== location.origin || url.pathname.startsWith("/api/")) return;
  event.respondWith((async () => {
    try {
      const response = await fetch(event.request);
      if (response.ok) await (await caches.open(cacheName)).put(event.request, response.clone());
      return response;
    } catch {
      const cached = await caches.match(event.request);
      if (cached) return cached;
      return new Response("Unavailable offline", { status: 503 });
    }
  })());
});
