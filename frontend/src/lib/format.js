export function escapeHtml(s) {
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}
export function firstLine(text) { return String(text || '').split('\n')[0] }
export function parseJSON(text) { try { return JSON.parse(text) } catch (e) { return null } }

function pick(o, keys) {
  for (const k of keys) { if (o && o[k] != null) return String(o[k]) }
  return ''
}

const SUMMARY_KEYS = ['command', 'file_path', 'path', 'pattern', 'url', 'query', 'description']
export function toolSummary(m) { return pick(parseJSON(m.text) || {}, SUMMARY_KEYS).split('\n')[0] }

// Escapes, then renders a small markdown subset (fenced/inline code, bold).
export function renderText(text) {
  const parts = String(text || '').split('```')
  let html = ''
  parts.forEach((part, i) => {
    if (i % 2 === 1) {
      const body = part.replace(/^[^\n]*\n/, (m) => (m.trim() ? '' : m))
      html += '<pre class="code mono"><code>' + escapeHtml(body.replace(/\n$/, '')) + '</code></pre>'
    } else {
      let seg = escapeHtml(part)
      seg = seg.replace(/`([^`]+)`/g, '<code>$1</code>')
      seg = seg.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
      html += seg
    }
  })
  return html
}

const PERM_ACTIONS = {
  Bash: 'Run command', Read: 'Read file', Edit: 'Edit file', Write: 'Write file',
  MultiEdit: 'Edit file', NotebookEdit: 'Edit notebook', Glob: 'Find files',
  Grep: 'Search', WebFetch: 'Fetch page', WebSearch: 'Web search',
}
export function permAction(p) { return PERM_ACTIONS[p.toolName] || p.toolName }
export function permSummary(p) { return pick(p.toolInput || {}, SUMMARY_KEYS).split('\n')[0] }
export function permDetails(p) { try { return JSON.stringify(p.toolInput, null, 2) } catch (e) { return '' } }

export function modelFamily(m) {
  const first = String(m || '').replace(/^claude-/, '').split(/[-\s]/)[0] || ''
  return first.charAt(0).toUpperCase() + first.slice(1).toLowerCase()
}

// Syncs model/effort chips from slash-command echoes; returns true to hide the line.
export function applyCommandState(text, state) {
  const mm = text.match(/^Set model to (.+?)(?: and saved.*)?$/)
  if (mm) { state.modelLabel = modelFamily(mm[1].trim()); return true }
  const em = text.match(/^(?:Set|Kept) effort level to (\w+)/)
  if (em) { state.effortLabel = em[1]; return true }
  return /^\/(model|effort)\b/.test(text)
}

export function baseName(p) {
  return p ? (p.replace(/\/+$/, '').split('/').pop() || p) : ''
}

export function timeAgo(ms) {
  if (!ms) return ''
  const s = Math.floor((Date.now() - ms) / 1000)
  if (s < 60) return 'just now'
  const m = Math.floor(s / 60); if (m < 60) return m + 'm ago'
  const h = Math.floor(m / 60); if (h < 24) return h + 'h ago'
  const d = Math.floor(h / 24); if (d < 7) return d + 'd ago'
  return new Date(ms).toLocaleDateString()
}

export function isImageType(type) { return /^image\//.test(type || '') }
export function fileExtLabel(name) {
  const m = /\.([A-Za-z0-9]{1,6})$/.exec(name || '')
  return m ? m[1].toUpperCase() : 'FILE'
}
