import MarkdownIt from 'markdown-it'

export function firstLine(text) { return String(text || '').split('\n')[0] }
export function parseJSON(text) { try { return JSON.parse(text) } catch (e) { return null } }

// Skips array/object values (e.g. Codex update_plan's `plan` is an array,
// while Claude's ExitPlanMode `plan` is a string) so a structurally different
// field under the same key never stringifies into "[object Object]".
function pick(o, keys) {
  for (const k of keys) {
    const v = o && o[k]
    if (v != null && typeof v !== 'object') return String(v)
  }
  return ''
}

// 'cmd' is Codex's exec tool; 'command' is Claude/opencode's Bash.
const SUMMARY_KEYS = ['command', 'cmd', 'file_path', 'path', 'pattern', 'url', 'query', 'description', 'plan']

// Most tool inputs reduce to one of SUMMARY_KEYS, but a few carry their
// headline in an array rather than a flat string field: Claude's
// AskUserQuestion/TodoWrite and Codex's structurally-equivalent
// request_user_input/update_plan.
function summarize(input) {
  if (!input) return ''
  if (Array.isArray(input.questions) && input.questions.length) {
    return input.questions.map((q) => q.question).join(' · ')
  }
  const steps = input.todos || input.plan
  if (Array.isArray(steps)) {
    return steps.length + (steps.length === 1 ? ' task' : ' tasks')
  }
  return pick(input, SUMMARY_KEYS).split('\n')[0]
}
export function toolSummary(m) { return summarize(parseJSON(m.text)) }

// Full CommonMark for message bubbles. html:false escapes any raw HTML in the
// text, which keeps v-html safe from injected markup.
const md = new MarkdownIt({ html: false, linkify: true, breaks: true })
export function renderText(text) { return md.render(String(text || '')) }

const PERM_ACTIONS = {
  // Claude Code / opencode
  Bash: 'Run command', Read: 'Read file', Edit: 'Edit file', Write: 'Write file',
  MultiEdit: 'Edit file', NotebookEdit: 'Edit notebook', Glob: 'Find files',
  Grep: 'Search', WebFetch: 'Fetch page', WebSearch: 'Web search',
  Task: 'Run agent', TodoWrite: 'Update tasks', AskUserQuestion: 'Question',
  ExitPlanMode: 'Exit plan mode', BashOutput: 'Read output', KillShell: 'Stop command',
  // Codex
  exec_command: 'Run command', write_stdin: 'Send input', wait: 'Wait',
  view_image: 'View image', request_user_input: 'Question', update_plan: 'Update tasks',
}
export function permAction(p) { return PERM_ACTIONS[p.toolName] || p.toolName }
export function permSummary(p) { return summarize(p.toolInput) }
export function permDetails(p) {
  const input = p.toolInput
  // Structural check (not a toolName allowlist) so it also covers Codex's
  // request_user_input, which carries the same questions/options shape.
  if (Array.isArray(input?.questions)) {
    return input.questions.map((q) => {
      const opts = (q.options || []).map((o) => `  - ${o.label}${o.description ? ': ' + o.description : ''}`)
      return [q.question, ...opts].join('\n')
    }).join('\n\n')
  }
  try { return JSON.stringify(input, null, 2) } catch (e) { return '' }
}

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

// Groups a timestamp into a coarse recency bucket for section headers. Compares
// calendar days (not elapsed hours) so yesterday morning reads as "Yesterday".
// History is capped to the last two weeks, so the tail bucket is just "Earlier".
export function dateBucket(ms) {
  if (!ms) return 'Earlier'
  const startOfDay = (d) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
  const today = startOfDay(new Date())
  const days = Math.round((today - startOfDay(new Date(ms))) / 86400000)
  if (days <= 0) return 'Today'
  if (days === 1) return 'Yesterday'
  if (days < 7) return 'Previous 7 days'
  return 'Earlier'
}

const CMD_LABELS = {
  clear: 'Context cleared', compact: 'Conversation compacted', init: 'Project initialized',
  model: 'Model changed', effort: 'Effort changed', resume: 'Session resumed',
  cost: 'Usage summary', help: 'Help', review: 'Review requested',
}
// A slash command surfaces two ways: a short command name ("clear", "/effort
// high") and a separate stdout blob that can be large. Both look broken crammed
// into a pill, so keep only a short human label — a friendly name for known
// commands, otherwise the bare token; never the multi-line body.
export function commandLabel(text) {
  const first = firstLine(text).trim()
  const tok = first.replace(/^\//, '').split(/\s+/)[0].toLowerCase()
  if (CMD_LABELS[tok]) return CMD_LABELS[tok]
  if (/^[a-z][a-z-]*$/.test(tok) && first.length <= 24) return '/' + tok
  return first.length > 72 ? first.slice(0, 72) + '…' : first
}

export function isToolMsg(m) { return !!m && (m.kind === 'tool_use' || m.kind === 'tool_result') }

// Folds runs of consecutive tool-activity messages into one collapsed group so a
// long stretch of Reads/Bash/Edits takes a single line instead of many. Lone
// tool calls are unwrapped back to their plain message.
export function groupMessages(messages) {
  const out = []
  let run = null
  for (const m of messages) {
    if (isToolMsg(m)) {
      if (!run) { run = { kind: 'tool_group', line: m.line, items: [] }; out.push(run) }
      run.items.push(m)
    } else {
      run = null
      out.push(m)
    }
  }
  return out.map((x) => (x.kind === 'tool_group' && x.items.length === 1 ? x.items[0] : x))
}

export function isImageType(type) { return /^image\//.test(type || '') }
export function fileExtLabel(name) {
  const m = /\.([A-Za-z0-9]{1,6})$/.exec(name || '')
  return m ? m[1].toUpperCase() : 'FILE'
}
