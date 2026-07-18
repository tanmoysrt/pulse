import { reactive, ref, computed, onUnmounted } from 'vue'
import { post } from '../lib/api'
import { applyCommandState, modelFamily } from '../lib/format'
import { MODES_BY_AGENT, STATIC_MODELS_BY_AGENT, EFFORTS_BY_AGENT, WORKING_VERBS } from '../constants'

// useSession owns one live session: SSE stream, reactive state, and actions.
export function useSession(id, seedAgent = '', seedTitle = '') {
  const base = '/api/sessions/' + id

  const state = reactive({
    status: 'connecting',
    title: seedTitle,
    agent: seedAgent,
    modelLabel: '',
    effortLabel: '',
    mode: '',
    todos: [],
    pending: null,
    started: false,
    closed: false,
    messages: [],
    queued: [],
    opencodeModels: [],
    modelsLoaded: false,
  })

  const seen = new Set()
  const elapsed = ref(0)
  let es = null
  let timer = null
  let modelsFetchStarted = false

  const isBusy = computed(() => state.status === 'running' || state.status === 'needs_approval')
  const modes = computed(() => MODES_BY_AGENT[state.agent] || [])
  const models = computed(() => (state.agent === 'opencode' ? state.opencodeModels : (STATIC_MODELS_BY_AGENT[state.agent] || [])))
  const efforts = computed(() => EFFORTS_BY_AGENT[state.agent] || [])
  const workingVerb = computed(() => WORKING_VERBS[Math.floor(elapsed.value / 4) % WORKING_VERBS.length])
  const appReady = computed(() => {
    if (state.closed) return true
    if (!state.started) return false
    if (state.status === 'connecting' || !state.modelsLoaded) return false
    if (state.agent === 'opencode' && !state.modelLabel) return false
    return true
  })

  function displayModel(m) { return state.agent === 'claude' ? modelFamily(m) : m }

  function onMessage(m) {
    if (seen.has(m.line)) return
    seen.add(m.line)
    if (m.kind === 'command' && applyCommandState(m.text, state)) return
    if (m.role === 'user' && m.kind === 'text' && state.queued.length) state.queued.shift()
    state.messages.push(m)
  }

  function onStatusChange(s, prev) {
    if (s === 'running' && prev !== 'running') {
      const start = Date.now(); elapsed.value = 0
      clearInterval(timer)
      timer = setInterval(() => { elapsed.value = Math.floor((Date.now() - start) / 1000) }, 1000)
    } else if (s !== 'running') {
      clearInterval(timer); timer = null
    }
  }

  function applyState(st) {
    const prev = state.status
    state.status = st.status
    if (typeof st.started === 'boolean') state.started = st.started
    if (st.agent) state.agent = st.agent
    if (st.title) state.title = st.title
    state.pending = st.pending
    state.mode = st.mode || state.mode
    if (st.effort) state.effortLabel = st.effort
    state.todos = st.todos || []
    if (st.model) state.modelLabel = displayModel(st.model)
    else if (st.modelName) state.modelLabel = displayModel(st.modelName)
    ensureModels()
    if (state.status !== prev) onStatusChange(state.status, prev)
  }

  function ensureModels() {
    if (modelsFetchStarted) return
    modelsFetchStarted = true
    if (state.agent !== 'opencode') { state.modelsLoaded = true; return }
    const poll = () => fetch(base + '/models').then((r) => r.json()).then((list) => {
      if (list && list.length) { state.opencodeModels = list; state.modelsLoaded = true }
      else setTimeout(poll, 400)
    }).catch(() => setTimeout(poll, 400))
    poll()
  }

  function connect() {
    es = new EventSource(base + '/events')
    es.addEventListener('message', (e) => onMessage(JSON.parse(e.data)))
    es.addEventListener('cleared', () => { state.messages = []; seen.clear() })
    es.addEventListener('closed', () => { disconnect(); state.closed = true })
    es.addEventListener('state', (e) => applyState(JSON.parse(e.data)))
    es.onerror = () => { if (state.status !== 'connecting') { const p = state.status; state.status = 'connecting'; onStatusChange('connecting', p) } }
  }
  function disconnect() { if (es) { es.close(); es = null } clearInterval(timer); timer = null }

  // actions
  const send = (text) => post(base + '/send', { text })
  const interrupt = () => post(base + '/interrupt')
  const decide = (pid, decision) => post(base + '/permission', { id: pid, decision })
  const clear = () => post(base + '/clear')
  const close = () => post(base + '/close')
  const setMode = (mode) => { state.mode = mode; return post(base + '/mode', { mode }) }
  const setModel = (o) => { state.modelLabel = o.label; return post(base + '/model', { model: o.id, label: o.label }) }
  const setEffort = (o) => { state.effortLabel = o.id; return post(base + '/effort', { level: o.id }) }
  function uploadFile(file) {
    const fd = new FormData(); fd.append('file', file)
    return fetch(base + '/upload', { method: 'POST', body: fd }).then((r) => { if (!r.ok) throw new Error('upload failed'); return r.json() })
  }

  onUnmounted(disconnect)

  return {
    state, elapsed, isBusy, modes, models, efforts, workingVerb, appReady,
    connect, disconnect,
    send, interrupt, decide, clear, close, setMode, setModel, setEffort, uploadFile,
  }
}
