import { createApp } from 'vue'
import router from './router'
import App from './App.vue'
import './lib/install' // side effect: captures beforeinstallprompt before anything else can miss it
import './style.css'

// Eagerly registered (not just when push is enabled) so an install prompt has
// an active service worker to point at from the very first load.
if (window.isSecureContext && 'serviceWorker' in navigator) {
  navigator.serviceWorker.register('/sw.js')
}

createApp(App).use(router).mount('#app')
