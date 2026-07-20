self.addEventListener('push', function (event) {
  var data = {}
  try { data = event.data ? event.data.json() : {} } catch (e) {}
  var url = data.url || '/'

  event.waitUntil(
    clients.matchAll({ type: 'window', includeUncontrolled: true }).then(function (list) {
      var focused = list.some(function (c) { return c.focused })
      if (focused) {
        // Already looking at the app — a chime is enough, skip the OS banner.
        list.forEach(function (c) { c.postMessage({ type: 'pulse-chime' }) })
        return
      }
      return self.registration.showNotification(data.title || 'pulse', {
        body: data.body || '',
        tag: 'pulse',
        renotify: true,
        icon: '/icons/icon-512.png',
        badge: '/icons/badge.png',
        data: { url: url },
      })
    })
  )
})

self.addEventListener('notificationclick', function (event) {
  event.notification.close()
  var url = (event.notification.data && event.notification.data.url) || '/'
  event.waitUntil(clients.matchAll({ type: 'window', includeUncontrolled: true }).then(function (list) {
    for (var i = 0; i < list.length; i++) {
      var c = list[i]
      if ('focus' in c) {
        c.postMessage({ type: 'pulse-open', url: url })
        return c.focus()
      }
    }
    if (clients.openWindow) return clients.openWindow('/#' + url)
  }))
})
