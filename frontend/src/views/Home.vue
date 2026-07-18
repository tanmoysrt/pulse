<template>
  <div class="home">
    <div class="home-head">
      <div class="home-brand"><span class="logo">P</span><span>Pulse</span></div>
      <div class="home-actions">
        <button v-if="pushSup" class="notif-btn" :class="{ on: pushOn }" :title="pushOn ? 'Notifications on — tap to turn off' : 'Enable notifications'" @click="toggleNotifs">
          <Icon :name="pushOn ? 'bell' : 'bell-off'" :size="16" />
        </button>
        <button class="new-btn" @click="showModal = true">
          <Icon name="plus" :size="15" />
          New chat
        </button>
      </div>
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
import { pushSupported, existingSubscription, enablePush, disablePush } from '../lib/push'
import NewChatModal from '../components/NewChatModal.vue'
import AgentLogo from '../components/AgentLogo.vue'
import Icon from '../components/Icon.vue'

const router = useRouter()
const live = ref([])
const history = ref([])
const loaded = ref(false)
const error = ref(false)
const showModal = ref(false)
const installed = ref([])

// Notifications are a daemon-level setting: one browser subscription covers
// every session, so it lives here rather than inside a chat.
const pushSup = pushSupported()
const pushOn = ref(false)
let pushReg = null
async function toggleNotifs() {
  try {
    if (pushOn.value) { await disablePush(pushReg); pushOn.value = false; return }
    const reg = await enablePush()
    if (reg) { pushReg = reg; pushOn.value = true }
    else window.alert('Notifications were blocked in the browser.')
  } catch (e) {
    window.alert('Notifications need an HTTPS connection (open the public link).')
  }
}

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
  if (pushSup) {
    const reg = await existingSubscription().catch(() => null)
    if (reg) { pushReg = reg; pushOn.value = true }
  }
})
</script>
