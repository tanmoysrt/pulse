<template>
  <div class="modal-backdrop" @click.self="$emit('close')">
    <div class="modal">
      <h3>New chat</h3>

      <div class="modal-label">Agent</div>
      <div class="seg">
        <button v-for="a in agents" :key="a" :class="{ on: agent === a }" @click="agent = a">
          <span class="seg-logo" :class="'agent-' + a"><AgentLogo :tool="a" /></span>{{ AGENT_LABELS[a] }}
        </button>
      </div>

      <div class="modal-label">Directory</div>
      <div class="dir-bar">
        <button class="dir-up" title="Parent" @click="load(parent)"><Icon name="arrow-up" :size="15" /></button>
        <span>{{ path || '…' }}</span>
      </div>
      <div class="dir-list">
        <div v-for="name in dirs" :key="name" class="dir-row" @click="enter(name)"><Icon name="folder" :size="15" />{{ name }}</div>
        <div v-if="!dirs.length" class="dir-empty">No subfolders — Start uses this directory</div>
      </div>

      <div class="modal-actions">
        <button class="modal-start" :disabled="!path || starting" @click="start">{{ starting ? 'Starting…' : 'Start' }}</button>
        <button class="modal-cancel" @click="$emit('close')">Cancel</button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { listDirs, spawnSession } from '../lib/api'
import { AGENTS, AGENT_LABELS } from '../constants'
import AgentLogo from './AgentLogo.vue'
import Icon from './Icon.vue'

const props = defineProps({ installed: { type: Array, default: () => [] } })
const emit = defineEmits(['close', 'started'])

const agents = computed(() => AGENTS.filter((a) => props.installed.includes(a)))
const agent = ref(agents.value[0] || 'claude')
const path = ref('')
const parent = ref('')
const dirs = ref([])
const starting = ref(false)

async function load(p) {
  try {
    const d = await listDirs(p)
    path.value = d.path
    parent.value = d.parent
    dirs.value = d.dirs || []
  } catch (e) { /* keep current */ }
}
function enter(name) { load(path.value.replace(/\/+$/, '') + '/' + name) }

async function start() {
  starting.value = true
  try {
    const d = await spawnSession(agent.value, path.value)
    emit('started', { id: d.id, agent: agent.value })
  } catch (err) {
    starting.value = false
    window.alert('Could not start: ' + err.message)
  }
}

onMounted(() => load(''))
</script>
