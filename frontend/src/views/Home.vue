<template>
  <div class="home">
    <div v-if="!loaded" class="boot-loader"><div class="pulse-rings"><span></span><span></span><span></span></div></div>

    <div class="home-head">
      <div class="home-brand"><span class="logo">P</span><span>Pulse</span></div>
      <div class="home-actions">
        <button v-if="hasAny" class="round-btn" :class="{ on: searchOpen }" title="Search chats" aria-label="Search chats" @click="toggleSearch">
          <Icon name="search" :size="16" />
        </button>
        <button v-if="pushSup" class="round-btn" :class="{ on: pushOn }" :title="pushOn ? 'Notifications on — tap to turn off' : 'Enable notifications'" @click="toggleNotifs">
          <Icon :name="pushOn ? 'bell' : 'bell-off'" :size="16" />
        </button>
        <button class="new-btn" @click="showModal = true">
          <Icon name="plus" :size="15" />
          New chat
        </button>
      </div>
    </div>

    <div v-if="searchOpen" class="home-search">
      <Icon name="search" :size="16" />
      <input ref="searchInput" v-model="query" type="search" placeholder="Search chats" aria-label="Search chats" @keydown.esc="closeSearch" />
      <button class="search-clear" aria-label="Close search" @click="closeSearch">
        <Icon name="x" :size="14" />
      </button>
    </div>

    <VirtualList v-if="rows.length" class="home-list" :items="rows" :item-key="(r) => r.key" :estimate="66">
      <template #default="{ item }">
        <div v-if="item.type === 'header'" class="home-section">{{ item.text }}</div>
        <button v-else class="card" @click="open(item.s, item.live)">
          <div class="agent-badge" :class="'agent-' + item.s.tool"><AgentLogo :tool="item.s.tool" /></div>
          <div class="card-main">
            <div class="card-title">{{ cardTitle(item.s) }}</div>
            <div class="card-sub">{{ item.s.dir }}</div>
          </div>
          <div class="card-meta">
            <span v-if="item.live" class="live-dot">live</span>
            <span v-else class="card-time">{{ timeAgo(item.s.updated) }}</span>
          </div>
        </button>
      </template>
    </VirtualList>
    <div v-else-if="error" class="home-list home-empty"><h2>Can’t reach pulse</h2><p>Is the daemon still running?</p></div>
    <div v-else-if="query" class="home-list home-empty"><h2>No matches</h2><p>Nothing matches “{{ query }}”.</p></div>
    <div v-else-if="loaded" class="home-list home-empty"><h2>No sessions yet</h2><p>Start one with “New chat”.</p></div>

    <NewChatModal v-if="showModal" :installed="installed" @close="showModal = false" @started="onStarted" />
  </div>
</template>

<script setup>
import { ref, computed, onMounted, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { listSessions } from '../lib/api'
import { AGENT_LABELS } from '../constants'
import { baseName, timeAgo, dateBucket } from '../lib/format'
import { pushSupported, existingSubscription, enablePush, disablePush } from '../lib/push'
import NewChatModal from '../components/NewChatModal.vue'
import AgentLogo from '../components/AgentLogo.vue'
import Icon from '../components/Icon.vue'
import VirtualList from '../components/VirtualList.vue'

const router = useRouter()
const live = ref([])
const history = ref([])
const loaded = ref(false)
const error = ref(false)
const showModal = ref(false)
const installed = ref([])
const query = ref('')
const searchOpen = ref(false)
const searchInput = ref(null)

const hasAny = computed(() => live.value.length > 0 || history.value.length > 0)

function toggleSearch() {
  searchOpen.value = !searchOpen.value
  if (searchOpen.value) nextTick(() => searchInput.value && searchInput.value.focus())
  else query.value = ''
}
function closeSearch() { searchOpen.value = false; query.value = '' }

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

function matches(s) {
  const q = query.value.trim().toLowerCase()
  if (!q) return true
  return cardTitle(s).toLowerCase().includes(q) || (s.dir || '').toLowerCase().includes(q)
}

// Flatten sections into one list the virtualizer can window over. Live sessions
// stay pinned under "Active"; history is bucketed by recency so the time span
// each transcript spans stays legible.
const rows = computed(() => {
  const out = []
  const liveRows = live.value.filter(matches)
  if (liveRows.length) {
    out.push({ type: 'header', text: 'Active', key: 'sec-active' })
    for (const s of liveRows) out.push({ type: 'card', s, live: true, key: 'l' + s.id })
  }
  let bucket = null
  for (const s of history.value) {
    if (!matches(s)) continue
    const label = dateBucket(s.updated)
    if (label !== bucket) {
      bucket = label
      out.push({ type: 'header', text: label, key: 'sec-' + label })
    }
    out.push({ type: 'card', s, live: false, key: 'h' + s.id })
  }
  return out
})

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
