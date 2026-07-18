<template>
  <div class="home">
    <div class="home-head">
      <div class="home-brand"><span class="logo">P</span><span>Pulse</span></div>
      <button class="new-btn" @click="showModal = true">
        <svg width="14" height="14" viewBox="0 0 16 16"><path d="M8 3v10M3 8h10" stroke="currentColor" stroke-width="2" stroke-linecap="round" /></svg>
        New chat
      </button>
    </div>

    <div class="home-list">
      <template v-if="live.length || history.length">
        <template v-if="live.length">
          <div class="home-section">Active</div>
          <button v-for="s in live" :key="s.id" class="card" @click="open(s, true)">
            <div class="agent-badge" :class="'agent-' + s.tool"><AgentLogo :tool="s.tool" /></div>
            <div class="card-main">
              <div class="card-title">{{ cardTitle(s) }}</div>
              <div class="card-sub">{{ s.dir }}</div>
            </div>
            <div class="card-meta"><span class="live-dot">live</span></div>
          </button>
        </template>
        <template v-if="history.length">
          <div class="home-section">History</div>
          <button v-for="s in history" :key="s.id" class="card" @click="open(s, false)">
            <div class="agent-badge" :class="'agent-' + s.tool"><AgentLogo :tool="s.tool" /></div>
            <div class="card-main">
              <div class="card-title">{{ cardTitle(s) }}</div>
              <div class="card-sub">{{ s.dir }}</div>
            </div>
            <div class="card-meta"><span class="card-time">{{ timeAgo(s.updated) }}</span></div>
          </button>
        </template>
      </template>
      <div v-else-if="error" class="home-empty"><h2>Can’t reach pulse</h2><p>Is the daemon still running?</p></div>
      <div v-else-if="loaded" class="home-empty"><h2>No sessions yet</h2><p>Start one with “New chat”.</p></div>
    </div>

    <NewChatModal v-if="showModal" :installed="installed" @close="showModal = false" @started="onStarted" />
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { listSessions } from '../lib/api'
import { AGENT_LABELS } from '../constants'
import { baseName, timeAgo } from '../lib/format'
import NewChatModal from '../components/NewChatModal.vue'
import AgentLogo from '../components/AgentLogo.vue'

const router = useRouter()
const live = ref([])
const history = ref([])
const loaded = ref(false)
const error = ref(false)
const showModal = ref(false)
const installed = ref([])

const cardTitle = (s) => s.title || baseName(s.dir) || AGENT_LABELS[s.tool] || 'Session'

function open(s, isLive) {
  const query = { a: s.tool, t: cardTitle(s) }
  if (isLive) router.push({ path: '/s/' + s.id, query })
  else router.push({ path: '/h/' + encodeURIComponent(s.id), query })
}
function onStarted(d) {
  showModal.value = false
  router.push({ path: '/s/' + d.id, query: { a: d.agent } })
}

onMounted(async () => {
  try {
    const d = await listSessions()
    live.value = d.live || []
    history.value = d.history || []
    installed.value = d.installed || []
  } catch (e) { error.value = true }
  loaded.value = true
})
</script>
