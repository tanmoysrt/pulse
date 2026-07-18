self.addEventListener('push', function (event) {
  var data = {}
  try { data = event.data ? event.data.json() : {} } catch (e) {}
  event.waitUntil(self.registration.showNotification(data.title || 'pulse', {
    body: data.body || '',
    tag: 'pulse',
    renotify: true,
  }))
})

self.addEventListener('notificationclick', function (event) {
  event.notification.close()
  event.waitUntil(clients.matchAll({ type: 'window', includeUncontrolled: true }).then(function (list) {
    for (var i = 0; i < list.length; i++) {
      if ('focus' in list[i]) return list[i].focus()
    }
    if (clients.openWindow) return clients.openWindow('/')
  }))
})
