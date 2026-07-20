<template>
  <div class="home">
    <div v-if="!loaded" class="boot-loader" role="status" aria-live="polite" aria-label="Loading">
      <div class="pulse-rings"><span></span><span></span><span></span></div>
    </div>

    <div class="home-head">
      <div class="home-brand"><PulseLogo class="logo" /><span>Pulse</span></div>
      <div class="home-actions">
        <button v-if="hasAny" class="round-btn" :class="{ on: searchOpen }" title="Search" aria-label="Search" @click="toggleSearch">
          <Icon name="search" :size="16" />
        </button>
        <button class="round-btn" title="Settings" aria-label="Settings" @click="showSettings = true">
          <Icon name="settings" :size="16" />
        </button>
      </div>
    </div>

    <div v-if="searchOpen" class="home-search">
      <Icon name="search" :size="15" />
      <input ref="searchInput" v-model="query" type="search" placeholder="Search chats" aria-label="Search chats" @keydown.esc="closeSearch" />
      <button v-if="query" class="search-clear" aria-label="Clear search" @click="query = ''">
        <Icon name="x" :size="13" />
      </button>
    </div>

    <VirtualList v-if="rows.length" class="home-list" :items="rows" :item-key="(r) => r.key" :estimate="66">
      <template #default="{ item }">
        <div v-if="item.type === 'header'" class="home-section">{{ item.text }}</div>
        <button v-else class="card" :class="{ flat: !item.live }" @click="open(item.s, item.live)">
          <div class="agent-badge" :class="'agent-' + item.s.tool">
            <AgentLogo :tool="item.s.tool" />
            <span v-if="item.live" class="live-corner" title="Live"></span>
          </div>
          <div class="card-main">
            <div class="card-title">{{ cardTitle(item.s) }}</div>
            <div class="card-sub">{{ item.s.dir }}</div>
          </div>
          <div class="card-meta">
            <span class="card-time">{{ timeAgo(item.s.updated) }}</span>
          </div>
        </button>
      </template>
    </VirtualList>
    <div v-else-if="error" class="home-list home-empty"><h2>Can’t reach pulse</h2><p>Is the daemon still running?</p></div>
    <div v-else-if="query" class="home-list home-empty"><h2>No matches</h2><p>Nothing matches “{{ query }}”.</p></div>
    <div v-else-if="loaded" class="home-list home-empty"><h2>No sessions yet</h2><p>Tap the button below to start one.</p></div>

    <button class="fab" title="New chat" aria-label="New chat" @click="showModal = true">
      <Icon name="plus" :size="22" />
    </button>

    <NewChatModal v-if="showModal" :installed="installed" @close="showModal = false" @started="onStarted" />
    <SettingsSheet v-if="showSettings" :push-supported="pushSup" :push-on="pushOn" @close="showSettings = false" @toggle-push="toggleNotifs" />
    <InstallPrompt v-if="showInstallPrompt" :ios="installIsIOS" @install="doInstall" @dismiss="dismissInstall" />
    <NotifyPrompt v-else-if="showNotifyPrompt" @enable="promptEnable" @dismiss="promptDismiss" />
  </div>
</template>

<script setup>
import { ref, computed, onMounted, nextTick, watch } from 'vue'
import { useRouter } from 'vue-router'
import { listSessions } from '../lib/api'
import { AGENT_LABELS } from '../constants'
import { baseName, timeAgo, dateBucket } from '../lib/format'
import { pushSupported, existingSubscription, enablePush, disablePush } from '../lib/push'
import { deferredPrompt, isStandalone, isIOSSafari, promptInstall } from '../lib/install'
import NewChatModal from '../components/NewChatModal.vue'
import SettingsSheet from '../components/SettingsSheet.vue'
import NotifyPrompt from '../components/NotifyPrompt.vue'
import InstallPrompt from '../components/InstallPrompt.vue'
import AgentLogo from '../components/AgentLogo.vue'
import Icon from '../components/Icon.vue'
import VirtualList from '../components/VirtualList.vue'
import PulseLogo from '../components/PulseLogo.vue'

const router = useRouter()
const live = ref([])
const history = ref([])
const loaded = ref(false)
const error = ref(false)
const showModal = ref(false)
const showSettings = ref(false)
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

// Notifications are a daemon-level setting (one browser subscription covers every
// session), so state lives here and is shared with the settings sheet + first-run
// prompt rather than inside a chat.
const pushSup = pushSupported()
const pushOn = ref(false)
const showNotifyPrompt = ref(false)
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

function dismissPrompt() { localStorage.setItem('pulse.notifPrompt', '1'); showNotifyPrompt.value = false; maybeOfferInstall() }
async function promptEnable() { dismissPrompt(); await toggleNotifs() }
function promptDismiss() { dismissPrompt() }

function maybeShowNotify() {
  if (pushSup && !pushOn.value && !error.value && !localStorage.getItem('pulse.notifPrompt')) showNotifyPrompt.value = true
}

// "Add to home screen": only ever over HTTPS, and only Chrome/Android (which
// fires beforeinstallprompt, often after this component has already mounted)
// or iOS Safari (which never fires it — manual Share-sheet instructions only).
const showInstallPrompt = ref(false)
const installIsIOS = ref(false)

function maybeOfferInstall() {
  if (!window.isSecureContext || isStandalone.value || showNotifyPrompt.value) return false
  if (localStorage.getItem('pulse.installPrompt')) return false
  if (isIOSSafari()) { installIsIOS.value = true; showInstallPrompt.value = true; return true }
  if (deferredPrompt.value) { installIsIOS.value = false; showInstallPrompt.value = true; return true }
  return false
}
watch(deferredPrompt, () => maybeOfferInstall())

function dismissInstall() { localStorage.setItem('pulse.installPrompt', '1'); showInstallPrompt.value = false; maybeShowNotify() }
async function doInstall() {
  await promptInstall()
  localStorage.setItem('pulse.installPrompt', '1')
  showInstallPrompt.value = false
  maybeShowNotify()
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
  const q = { a: s.tool, t: cardTitle(s) }
  if (isLive) router.push({ path: '/s/' + s.id, query: q })
  else router.push({ path: '/h/' + encodeURIComponent(s.id), query: q })
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
  // Install takes priority (Chrome's beforeinstallprompt may not have fired
  // yet, in which case the watcher above picks it up later); notifications
  // only get offered once there's nothing to install.
  if (!maybeOfferInstall()) maybeShowNotify()
})
</script>
