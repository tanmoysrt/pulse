export function post(url, body) {
  return fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: body ? JSON.stringify(body) : undefined,
  })
}

// A 401 means the token/cookie is missing or stale; send the user to the login
// page, remembering where they were so login can return them there.
function redirectToLogin() {
  const after = window.location.hash.slice(1) || '/'
  if (after.startsWith('/login')) return
  sessionStorage.setItem('pulse.after', after)
  window.location.hash = '#/login'
}

export async function getJSON(url) {
  const r = await fetch(url)
  if (r.status === 401) { redirectToLogin(); throw new Error('unauthorized') }
  if (!r.ok) throw new Error('HTTP ' + r.status)
  return r.json()
}

// login posts the password; returns { ok } or, when rate-limited, { retryAfter }.
export async function login(password) {
  const r = await post('/api/login', { password })
  if (r.ok) return { ok: true }
  const d = await r.json().catch(() => ({}))
  return { ok: false, status: r.status, retryAfter: d.retryAfter }
}

export const getStats = () => getJSON('/api/stats')
export const logout = () => post('/api/logout')

export const listSessions = () => getJSON('/api/sessions')
// readHistory returns a page ending at `before` (exclusive), or the latest page.
export const readHistory = (ref, before) =>
  getJSON('/api/history?ref=' + encodeURIComponent(ref) + (before != null ? '&before=' + before : ''))
export const listDirs = (path) => getJSON('/api/dirs' + (path ? '?path=' + encodeURIComponent(path) : ''))

export async function spawnSession(agent, dir) {
  const r = await post('/api/sessions', { agent, dir })
  const d = await r.json().catch(() => ({}))
  if (!r.ok || !d.id) throw new Error(d.error || 'spawn failed')
  return d
}

export async function resumeSession(ref) {
  const r = await post('/api/sessions', { resume: ref })
  const d = await r.json().catch(() => ({}))
  if (!r.ok || !d.id) throw new Error(d.error || 'resume failed')
  return d
}
