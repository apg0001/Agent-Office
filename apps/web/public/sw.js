// PWA 서비스 워커(#255). 설치형 앱으로 열 수 있게 하고, 같은 출처의 정적 파일만 캐시한다.
// API(`/api/`)와 화면 HTML은 캐시하지 않는다: 세션·계정 정보가 기기 저장소에 남지 않게 한다.

const CACHE = "agent-office-static-v1";

self.addEventListener("install", () => {
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((key) => key !== CACHE).map((key) => caches.delete(key))))
      .then(() => self.clients.claim()),
  );
});

/** 파일 이름에 해시가 있어 내용이 바뀌지 않는 빌드 산출물. */
function immutable(path) {
  return path.startsWith("/assets/");
}

/** 해시는 없지만 바뀔 일이 드문 정적 파일. 캐시로 바로 보여 주고 뒤에서 새로 받는다. */
function staticFile(path) {
  return /^\/(fonts|office|icons)\//.test(path) || path === "/favicon.svg";
}

async function fromCache(request, refresh) {
  const cache = await caches.open(CACHE);
  const hit = await cache.match(request);
  if (hit && !refresh) return hit;
  const network = fetch(request).then((response) => {
    if (response.ok) cache.put(request, response.clone());
    return response;
  });
  if (hit) {
    network.catch(() => undefined);
    return hit;
  }
  return network;
}

self.addEventListener("fetch", (event) => {
  const { request } = event;
  if (request.method !== "GET") return;
  const url = new URL(request.url);
  if (url.origin !== self.location.origin || url.pathname.startsWith("/api/")) return;
  if (immutable(url.pathname)) {
    event.respondWith(fromCache(request, false));
  } else if (staticFile(url.pathname)) {
    event.respondWith(fromCache(request, true));
  }
});
