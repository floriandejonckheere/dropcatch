// Dropcatch's service worker. It shows push notifications (a domain or VPS
// became available), keeps the app shell so that the installed app opens
// when the server is unreachable, and, where the browser runs periodic
// background sync (Chrome for installed apps), checks whether the server
// can be reached and notifies when that changes.
//
// It is served as is, not bundled, so it is plain JavaScript.

const SHELL = 'dropcatch-shell-v1'
const STATE = 'dropcatch-state'
const scope = self.registration.scope
const at = (path) => new URL(path, scope).href

self.addEventListener('install', (event) => {
  event.waitUntil(caches.open(SHELL).then((c) => c.addAll(['./', 'manifest.webmanifest', 'favicon.svg', 'icon-192.png'].map(at))))
  self.skipWaiting()
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    (async () => {
      for (const key of await caches.keys()) if (key !== SHELL && key !== STATE) await caches.delete(key)
      await self.clients.claim()
    })(),
  )
})

self.addEventListener('fetch', (event) => {
  const req = event.request
  const url = new URL(req.url)
  // The API (including the event stream) always goes to the server.
  if (req.method !== 'GET' || !url.href.startsWith(scope) || url.href.startsWith(at('api/'))) return
  if (req.mode === 'navigate') {
    // The page from the server when it answers, so updates arrive at once.
    event.respondWith(
      fetch(req)
        .then((res) => {
          if (res.ok) void keep(res.clone())
          return res
        })
        .catch(async () => (await caches.match(at('./'))) ?? Response.error()),
    )
    return
  }
  // Scripts and styles have content-hashed names, so a cached copy is
  // never stale; the old ones go with the next shell version.
  event.respondWith(
    (async () => {
      const cached = await caches.match(req)
      if (cached) return cached
      const res = await fetch(req)
      if (res.ok && url.pathname.includes('/assets/')) void caches.open(SHELL).then((c) => c.put(req, res.clone()))
      return res
    })(),
  )
})

// keep stores the page and drops the assets of earlier versions, which it
// no longer references.
async function keep(res) {
  const html = await res.clone().text()
  const cache = await caches.open(SHELL)
  await cache.put(at('./'), res)
  for (const req of await cache.keys()) {
    const name = new URL(req.url).pathname.split('/').pop()
    if (req.url.includes('/assets/') && !html.includes(name)) await cache.delete(req)
  }
}

self.addEventListener('push', (event) => {
  let msg = {}
  try {
    msg = event.data?.json() ?? {}
  } catch {
    msg = { title: 'Dropcatch', body: event.data?.text() ?? '' }
  }
  event.waitUntil(notify(msg.title || 'Dropcatch', msg.body, msg.tag, msg.view))
})

function notify(title, body, tag, view) {
  return self.registration.showNotification(title, {
    body,
    tag: tag || undefined,
    // A newer message about the same domain or VPS alerts again.
    renotify: Boolean(tag),
    icon: at('icon-192.png'),
    badge: at('icon-192.png'),
    data: { view: view || '' },
  })
}

self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  const target = at(event.notification.data?.view ? `#/${event.notification.data.view}` : './')
  event.waitUntil(
    (async () => {
      const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
      const open = windows.find((w) => w.url.startsWith(scope))
      if (open) {
        await open.focus()
        return open.navigate(target)
      }
      return self.clients.openWindow(target)
    })(),
  )
})

self.addEventListener('periodicsync', (event) => {
  if (event.tag === 'health') event.waitUntil(checkHealth())
})

// checkHealth asks the server whether it is up and notifies once when it
// stops answering and once when it is back. The last result is kept in
// the cache, as service workers lose their variables when they stop.
async function checkHealth() {
  let up = false
  try {
    up = (await fetch(at('api/config'), { cache: 'no-store' })).ok
  } catch {
    // unreachable
  }
  const cache = await caches.open(STATE)
  const last = await cache.match('health')
  const wasUp = last ? (await last.json()).up : true
  await cache.put('health', new Response(JSON.stringify({ up, at: Date.now() })))
  if (up === wasUp) return
  if (!up) await notify('Dropcatch is unreachable', 'The server does not answer, so domains and VPS are not being checked.', 'health', '')
  else await notify('Dropcatch is back', 'The server answers again.', 'health', '')
}
