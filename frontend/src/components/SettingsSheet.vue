<template>
  <Teleport to="body">
    <div class="sheet-backdrop" @click.self="close">
      <div ref="root" class="settings-sheet" role="dialog" aria-modal="true" aria-label="Settings" tabindex="-1">
        <div class="sheet-head">
          <h3>Settings</h3>
          <button class="icon-btn" aria-label="Close" @click="close"><Icon name="x" :size="18" /></button>
        </div>

        <div class="sheet-body">
            <div class="set-row">
              <div class="set-row-main">
                <div class="set-row-title">Push notifications</div>
                <div class="set-row-sub">{{ pushSupported ? 'Alerts on this device when an agent needs you' : 'Needs an https connection (open the public link)' }}</div>
              </div>
              <button
                class="toggle" :class="{ on: pushOn }" :disabled="!pushSupported"
                role="switch" :aria-checked="pushOn" aria-label="Push notifications"
                @click="$emit('toggle-push')"
              ><span class="toggle-knob"></span></button>
            </div>

            <div class="set-row">
              <div class="set-row-main">
                <div class="set-row-title">Version</div>
                <div class="set-row-sub">{{ ver.current || '…' }}<span v-if="notice"> · {{ notice }}</span></div>
              </div>
              <button
                v-if="!ver.tunnel"
                class="update-pill" :disabled="isDev || checking"
                :title="isDev ? 'Dev builds can\'t self-update' : ''"
                @click="onUpdateClick"
              >{{ updateLabel }}</button>
            </div>

            <div v-for="m in metrics" :key="m.key" class="stat">
              <div class="stat-head">
                <span class="stat-icon" :style="{ color: m.color }"><Icon :name="m.icon" :size="15" /></span>
                <span class="stat-label">{{ m.label }}</span>
                <span v-if="hasStats" class="stat-value">{{ m.display }}</span>
                <span v-else class="skeleton skeleton-val"></span>
              </div>
              <Sparkline v-if="hasStats" :values="m.values" :max="m.max" :color="m.color" />
              <div v-else class="skeleton skeleton-spark"></div>
            </div>

          <button class="btn btn-ghost btn-block set-logout" @click="doLogout">Log out</button>
        </div>
      </div>
    </div>
  </Teleport>

  <Teleport to="body">
    <div v-if="showConfirm" class="modal-backdrop" @click.self="!updating && (showConfirm = false)">
      <div class="modal" role="dialog" aria-modal="true" aria-label="Update pulse">
        <h3>Update pulse?</h3>
        <p class="modal-copy">{{ ver.current }} → {{ ver.latest }}. Sessions keep running — pulse restarts itself in the background.</p>
        <p v-if="updateError" class="modal-copy modal-error">{{ updateError }}</p>
        <div class="modal-actions">
          <button class="btn btn-primary" :disabled="updating" @click="confirmUpdate">{{ updating ? updateStep : 'Update' }}</button>
          <button class="btn btn-ghost" :disabled="updating" @click="showConfirm = false">Cancel</button>
        </div>
      </div>
    </div>
  </Teleport>

  <Teleport to="body">
    <div v-if="showUpToDate" class="modal-backdrop" @click.self="showUpToDate = false">
      <div class="modal" role="dialog" aria-modal="true" aria-label="No update available">
        <h3>You're up to date</h3>
        <p class="modal-copy">Pulse {{ ver.current }} is the latest version.</p>
        <div class="modal-actions">
          <button class="btn btn-primary" @click="showUpToDate = false">OK</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { getStats, getVersion, requestUpdate, logout } from '../lib/api'
import { useModal } from '../composables/useModal'
import Icon from './Icon.vue'
import Sparkline from './Sparkline.vue'

defineProps({ pushSupported: Boolean, pushOn: Boolean })
const emit = defineEmits(['close', 'toggle-push'])

const router = useRouter()
const root = ref(null)
function close() { emit('close') }
useModal(root, close)
const samples = ref([])
const ver = ref({})
let poll = null

const hasStats = computed(() => samples.value.length > 1)

function fmtRate(bps) {
  if (bps < 1024) return Math.round(bps) + ' B/s'
  if (bps < 1024 * 1024) return (bps / 1024).toFixed(0) + ' KB/s'
  return (bps / 1024 / 1024).toFixed(1) + ' MB/s'
}

const metrics = computed(() => {
  const s = samples.value
  const last = s[s.length - 1] || {}
  return [
    { key: 'cpu', icon: 'cpu', label: 'CPU', color: '#8b5cf6', max: 100,
      values: s.map((x) => x.cpu), display: Math.round(last.cpu || 0) + '%' },
    { key: 'mem', icon: 'memory', label: 'Memory', color: '#0ea5e9', max: 100,
      values: s.map((x) => x.mem), display: Math.round(last.mem || 0) + '%' },
    { key: 'net', icon: 'activity', label: 'Network', color: '#10b981', max: null,
      values: s.map((x) => (x.rx || 0) + (x.tx || 0)),
      display: '↓ ' + fmtRate(last.rx || 0) + '   ↑ ' + fmtRate(last.tx || 0) },
  ]
})

async function refresh() {
  try { samples.value = (await getStats()).samples || [] } catch (e) { /* keep last */ }
}
async function doLogout() {
  try { await logout() } catch (e) { /* clear locally anyway */ }
  router.replace('/login')
}

async function refreshVersion() {
  try { ver.value = await getVersion() } catch (e) { /* no version info this time */ }
}

const isDev = computed(() => ver.value.current === 'dev')
const checking = ref(false)
const notice = ref('')
const showConfirm = ref(false)
const showUpToDate = ref(false)
const updating = ref(false)
const updateStep = ref('Updating…')
const updateError = ref('')

const updateLabel = computed(() => {
  if (isDev.value) return 'Update'
  if (checking.value) return 'Checking…'
  if (ver.value.available) return 'Update to ' + ver.value.latest
  return 'Check'
})

async function onUpdateClick() {
  checking.value = true
  try { ver.value = await getVersion(true) } catch (e) { /* keep last known */ }
  checking.value = false
  if (ver.value.available) showConfirm.value = true
  else showUpToDate.value = true
}

async function confirmUpdate() {
  updating.value = true
  updateError.value = ''
  updateStep.value = 'Updating…'
  try {
    await requestUpdate()
    updateStep.value = 'Restarting…'
    await waitForReconnect()
    showConfirm.value = false
    notice.value = 'updated to ' + ver.value.latest
    refreshVersion()
  } catch (e) {
    updateError.value = e.message || 'update failed'
  } finally {
    updating.value = false
  }
}

// The daemon drops off for a moment mid-restart; give it time to actually
// go down before polling, then wait for it to answer again on the new build.
async function waitForReconnect() {
  await new Promise((r) => setTimeout(r, 800))
  for (let i = 0; i < 30; i++) {
    try {
      if ((await getVersion()).current) return
    } catch (e) { /* not back yet */ }
    await new Promise((r) => setTimeout(r, 1000))
  }
  throw new Error('restarted, but pulse has not come back online yet')
}

onMounted(() => { refresh(); poll = setInterval(refresh, 5000); refreshVersion() })
onUnmounted(() => clearInterval(poll))
</script>
