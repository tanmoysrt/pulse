<template>
  <header :class="{ shadow }">
    <div class="brand">
      <button class="backbtn" title="Back" aria-label="Back" @click="$emit('back')">
        <Icon name="chevron-left" :size="18" />
      </button>
      <span class="title">{{ title || 'Pulse' }}</span>
      <span v-if="!readonly && status === 'connecting'" class="status-badge">Connecting</span>
      <button v-if="resumable" class="resume-btn" @click="$emit('resume')">
        <Icon name="play" :size="14" />
        Resume
      </button>
    </div>

    <div class="header-right">
      <button v-if="todos.length" class="tasks-chip" :aria-expanded="tasksOpen" aria-controls="tasksSheet" @click="$emit('toggleTasks')">
        <span class="tasks-progress">{{ done }}/{{ todos.length }}</span>
        <span class="tasks-now">{{ currentTask }}</span>
        <Chevron :up="tasksOpen" />
      </button>

      <div class="kebab-wrap" ref="root">
        <button class="kebab" :class="{ active: open }" title="Session menu" aria-label="Session menu"
          aria-haspopup="menu" :aria-expanded="open" @click="toggle">
          <Icon name="ellipsis-vertical" :size="16" />
        </button>
        <div v-if="open" class="kebab-menu" role="menu" @keydown.esc="close">
          <button class="menuitem" role="menuitem" @click="run('compact')">Compact chat</button>
          <button class="menuitem danger" role="menuitem" @click="run('clear')">Clear chat</button>
          <button class="menuitem danger" role="menuitem" @click="run('close')">Close session</button>
        </div>
      </div>
    </div>
  </header>
</template>

<script setup>
import { ref, computed, onBeforeUnmount } from 'vue'
import Chevron from './Chevron.vue'
import Icon from './Icon.vue'

const props = defineProps({
  title: String, status: String, todos: { type: Array, default: () => [] },
  tasksOpen: Boolean, readonly: Boolean, shadow: Boolean, resumable: Boolean,
})
const emit = defineEmits(['back', 'toggleTasks', 'clear', 'compact', 'close', 'resume'])

const open = ref(false)
const root = ref(null)

const done = computed(() => props.todos.filter((t) => t.status === 'completed').length)
const currentTask = computed(() => {
  const running = props.todos.find((x) => x.status === 'in_progress')
  if (running) return 'Working: ' + running.content
  const next = props.todos.find((x) => x.status !== 'completed')
  return next ? next.content : 'All tasks done'
})

function toggle() {
  open.value = !open.value
  if (open.value) document.addEventListener('mousedown', onAway)
  else document.removeEventListener('mousedown', onAway)
}
function close() { open.value = false; document.removeEventListener('mousedown', onAway) }
function onAway(e) { if (root.value && !root.value.contains(e.target)) close() }
function run(action) { close(); emit(action) }
onBeforeUnmount(() => document.removeEventListener('mousedown', onAway))
</script>
