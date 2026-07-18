export function post(url, body) {
  return fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: body ? JSON.stringify(body) : undefined,
  })
}

export async function getJSON(url) {
  const r = await fetch(url)
  if (!r.ok) throw new Error('HTTP ' + r.status)
  return r.json()
}

export const listSessions = () => getJSON('/api/sessions')
export const readHistory = (ref) => getJSON('/api/history?ref=' + encodeURIComponent(ref))
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
