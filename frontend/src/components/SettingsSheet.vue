<template>
  <Teleport to="body">
    <div class="sheet-backdrop" @click.self="$emit('close')">
      <div class="settings-sheet" role="dialog" aria-label="Settings">
        <div class="sheet-head">
          <h3>Settings</h3>
          <button class="icon-btn" aria-label="Close" @click="$emit('close')"><Icon name="x" :size="18" /></button>
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

          <div class="set-divider"></div>

          <div class="set-section">System</div>
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

          <div class="set-divider"></div>

          <div class="set-row">
            <div class="set-row-main">
              <div class="set-row-sub">Signed in as</div>
              <div class="set-row-title">{{ user || '…' }}</div>
            </div>
            <button class="btn btn-ghost" @click="doLogout">Log out</button>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { getStats, getMe, logout } from '../lib/api'
import Icon from './Icon.vue'
import Sparkline from './Sparkline.vue'

defineProps({ pushSupported: Boolean, pushOn: Boolean })
defineEmits(['close', 'toggle-push'])

const router = useRouter()
const samples = ref([])
const user = ref('')
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
      display: 'down ' + fmtRate(last.rx || 0) + '  up ' + fmtRate(last.tx || 0) },
  ]
})

async function refresh() {
  try { samples.value = (await getStats()).samples || [] } catch (e) { /* keep last */ }
}
async function doLogout() {
  try { await logout() } catch (e) { /* clear locally anyway */ }
  router.replace('/login')
}

onMounted(async () => {
  refresh()
  poll = setInterval(refresh, 5000)
  try { user.value = (await getMe()).user || '' } catch (e) { /* leave blank */ }
})
onUnmounted(() => clearInterval(poll))
</script>
