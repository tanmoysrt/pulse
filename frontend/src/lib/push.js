// Web-push helpers; VAPID key + subscriptions are daemon-level (not per-session).
import { post, getJSON } from './api'

export function pushSupported() {
  return window.isSecureContext && 'serviceWorker' in navigator &&
    'PushManager' in window && 'Notification' in window
}

function urlB64(s) {
  const pad = '='.repeat((4 - (s.length % 4)) % 4)
  const raw = atob((s + pad).replace(/-/g, '+').replace(/_/g, '/'))
  const arr = new Uint8Array(raw.length)
  for (let i = 0; i < raw.length; i++) arr[i] = raw.charCodeAt(i)
  return arr
}

// Returns the SW registration if a subscription already exists, re-posting it.
export async function existingSubscription() {
  if (!pushSupported() || Notification.permission !== 'granted') return null
  const reg = await navigator.serviceWorker.register('/sw.js')
  const sub = await reg.pushManager.getSubscription()
  if (sub) post('/api/push/subscribe', sub)
  return sub ? reg : null
}

export async function enablePush() {
  if (!pushSupported()) throw new Error('insecure-context')
  const perm = await Notification.requestPermission()
  if (perm !== 'granted') return null
  const reg = await navigator.serviceWorker.register('/sw.js')
  const { key } = await getJSON('/api/push/key')
  const sub = await reg.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: urlB64(key),
  })
  await post('/api/push/subscribe', sub)
  return reg
}

export async function disablePush(reg) {
  if (!reg) return
  const sub = await reg.pushManager.getSubscription()
  if (sub) sub.unsubscribe()
}
