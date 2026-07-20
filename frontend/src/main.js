import { createApp } from 'vue'
import router from './router'
import App from './App.vue'
import './lib/install' // side effect: captures beforeinstallprompt before anything else can miss it
import { playChime } from './lib/chime'
import './style.css'

// Eagerly registered (not just when push is enabled) so an install prompt has
// an active service worker to point at from the very first load.
if (window.isSecureContext && 'serviceWorker' in navigator) {
  navigator.serviceWorker.register('/sw.js')
  // sw.js talks back for two cases: the tab was focused when a push arrived
  // (chime instead of an OS banner), or a notification was clicked while a
  // tab was already open (navigate in place instead of opening a new one).
  navigator.serviceWorker.addEventListener('message', (event) => {
    if (event.data?.type === 'pulse-chime') playChime()
    else if (event.data?.type === 'pulse-open') router.push(event.data.url)
  })
}

createApp(App).use(router).mount('#app')
