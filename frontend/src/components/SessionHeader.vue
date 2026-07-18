<template>
  <header :class="{ shadow }">
    <div class="brand">
      <button class="backbtn" title="Back" aria-label="Back" @click="$emit('back')">
        <svg width="16" height="16" viewBox="0 0 16 16"><path d="M10 3L5 8l5 5" stroke="currentColor" stroke-width="1.8" fill="none" stroke-linecap="round" stroke-linejoin="round" /></svg>
      </button>
      <span class="title">{{ title || 'Pulse' }}</span>
      <span v-if="!readonly && status === 'connecting'" class="status-badge">Connecting</span>
      <button v-if="resumable" class="resume-btn" @click="$emit('resume')">
        <svg width="13" height="13" viewBox="0 0 16 16"><path d="M2.5 8a5.5 5.5 0 1 1 1.6 3.9M2.5 12V8.5H6" stroke="currentColor" stroke-width="1.6" fill="none" stroke-linecap="round" stroke-linejoin="round" /></svg>
        Resume
      </button>
    </div>

    <div class="header-right">
      <div v-if="todos.length" class="tasks-chip" @click="$emit('toggleTasks')">
        <span class="tasks-progress">{{ done }}/{{ todos.length }}</span>
        <span class="tasks-now">{{ currentTask }}</span>
        <Chevron :up="tasksOpen" />
      </div>

      <div class="kebab-wrap" ref="root">
        <button class="kebab" :class="{ active: open }" title="Session menu" aria-label="Session menu" @click="toggle">
          <svg width="12" height="12" viewBox="0 0 16 16"><circle cx="8" cy="3" r="1.4" fill="currentColor" /><circle cx="8" cy="8" r="1.4" fill="currentColor" /><circle cx="8" cy="13" r="1.4" fill="currentColor" /></svg>
        </button>
        <div v-if="open" class="kebab-menu">
          <button v-if="pushSupported" class="menuitem" @click="run('togglePush')">{{ pushOn ? 'Turn off notifications' : 'Enable notifications' }}</button>
          <button class="menuitem" @click="run('clear')">Clear chat</button>
          <button class="menuitem danger" @click="run('close')">Close session</button>
        </div>
      </div>
    </div>
  </header>
</template>

<script setup>
import { ref, computed, onBeforeUnmount } from 'vue'
import Chevron from './Chevron.vue'

const props = defineProps({
  title: String, status: String, todos: { type: Array, default: () => [] },
  tasksOpen: Boolean, readonly: Boolean, pushOn: Boolean, pushSupported: Boolean, shadow: Boolean, resumable: Boolean,
})
const emit = defineEmits(['back', 'toggleTasks', 'clear', 'close', 'togglePush', 'resume'])

const open = ref(false)
const root = ref(null)

const done = computed(() => props.todos.filter((t) => t.status === 'completed').length)
const currentTask = computed(() => {
  const t = props.todos.find((x) => x.status === 'in_progress') || props.todos.find((x) => x.status !== 'completed')
  return t ? t.content : 'All tasks done'
})

function toggle() {
  open.value = !open.value
  if (open.value) document.addEventListener('mousedown', onAway)
  else document.removeEventListener('mousedown', onAway)
}
function onAway(e) { if (root.value && !root.value.contains(e.target)) { open.value = false; document.removeEventListener('mousedown', onAway) } }
function run(action) { open.value = false; document.removeEventListener('mousedown', onAway); emit(action) }
onBeforeUnmount(() => document.removeEventListener('mousedown', onAway))
</script>
