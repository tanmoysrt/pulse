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
      <div v-if="todos.length" class="tasks-chip" @click="$emit('toggleTasks')">
        <span class="tasks-progress">{{ done }}/{{ todos.length }}</span>
        <span class="tasks-now">{{ currentTask }}</span>
        <Chevron :up="tasksOpen" />
      </div>

      <div class="kebab-wrap" ref="root">
        <button class="kebab" :class="{ active: open }" title="Session menu" aria-label="Session menu" @click="toggle">
          <Icon name="ellipsis-vertical" :size="16" />
        </button>
        <div v-if="open" class="kebab-menu">
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
import Icon from './Icon.vue'

const props = defineProps({
  title: String, status: String, todos: { type: Array, default: () => [] },
  tasksOpen: Boolean, readonly: Boolean, shadow: Boolean, resumable: Boolean,
})
const emit = defineEmits(['back', 'toggleTasks', 'clear', 'close', 'resume'])

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
