self.addEventListener('push', function (event) {
  var data = {}
  try { data = event.data ? event.data.json() : {} } catch (e) {}
  var url = data.url || '/'

  event.waitUntil(
    clients.matchAll({ type: 'window', includeUncontrolled: true }).then(function (list) {
      var focused = list.find(function (c) { return c.focused })
      if (focused) {
        // On a chat/history screen, they're already watching it live — stay
        // silent. On the list (or anything else), a chime is enough; either
        // way skip the OS banner since they're looking right at the app.
        if (!/#\/(s|h)\//.test(focused.url)) focused.postMessage({ type: 'pulse-chime' })
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
