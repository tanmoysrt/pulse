// "Add to home screen" support. `beforeinstallprompt` fires early and only
// once per page load, so it's captured here at module scope (loaded from
// main.js) rather than inside a component that mounts later and could miss it.
import { ref } from 'vue'

export const deferredPrompt = ref(null)
export const justInstalled = ref(false)

function standalone() {
  return window.matchMedia('(display-mode: standalone)').matches || window.navigator.standalone === true
}

export const isStandalone = ref(standalone())

if (window.isSecureContext) {
  window.addEventListener('beforeinstallprompt', (e) => {
    e.preventDefault()
    deferredPrompt.value = e
  })
  window.addEventListener('appinstalled', () => {
    deferredPrompt.value = null
    justInstalled.value = true
    isStandalone.value = true
  })
}

// iOS never fires beforeinstallprompt; Safari is the only iOS browser that
// can install at all (Chrome/Firefox-on-iOS are just Safari skins without it).
export function isIOSSafari() {
  const ua = navigator.userAgent
  const ios = /iP(hone|od|ad)/.test(ua) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)
  return ios && /Safari/.test(ua) && !/CriOS|FxiOS|EdgiOS|OPiOS/.test(ua)
}

// True once there's something installable to show: Chrome's native prompt is
// ready, or we're on iOS Safari where the only path is manual instructions.
export function canPromptInstall() {
  return window.isSecureContext && !isStandalone.value && (deferredPrompt.value != null || isIOSSafari())
}

// Resolves once the browser's own install dialog is answered; null on iOS
// (there's no programmatic prompt) or once android already consumed it.
export async function promptInstall() {
  const evt = deferredPrompt.value
  if (!evt) return null
  deferredPrompt.value = null
  evt.prompt()
  return evt.userChoice
}
