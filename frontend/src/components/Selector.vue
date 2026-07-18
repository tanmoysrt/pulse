<template>
  <div class="sel-wrap" ref="root" style="position: relative;">
    <button class="sel" :class="{ active: open, cap }" @click="toggle">
      <span class="sel-label">{{ label }}</span>
      <Chevron />
    </button>
    <div v-if="open" ref="menu" class="menu" :class="{ combo: searchable }" @click.stop>
      <input v-if="searchable" ref="search" class="combo-input" placeholder="Search…" v-model="query"
        @keydown.enter.prevent="enterFirst" @keydown.esc="close" />
      <div :class="searchable ? 'combo-list' : ''">
        <button v-for="o in filtered" :key="o.id" :class="{ current: isCurrent(o) }" @click="choose(o)">{{ o.label }}</button>
        <div v-if="searchable && !filtered.length" class="combo-empty">No matches</div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, nextTick, onBeforeUnmount } from 'vue'
import Chevron from './Chevron.vue'

const props = defineProps({
  label: String,
  options: { type: Array, default: () => [] },
  current: { type: [String, Function], default: '' },
  searchable: Boolean,
  cap: Boolean,
})
const emit = defineEmits(['select'])

const open = ref(false)
const query = ref('')
const root = ref(null)
const menu = ref(null)
const search = ref(null)

const filtered = computed(() => {
  const q = query.value.trim().toLowerCase()
  return q ? props.options.filter((o) => o.label.toLowerCase().includes(q)) : props.options
})
function isCurrent(o) {
  return typeof props.current === 'function' ? props.current(o) : props.current === o.label
}
function choose(o) { emit('select', o); close() }
function enterFirst() { if (filtered.value[0]) choose(filtered.value[0]) }
function close() { open.value = false; document.removeEventListener('mousedown', onAway) }
function toggle() {
  open.value = !open.value
  if (!open.value) { document.removeEventListener('mousedown', onAway); return }
  query.value = ''
  document.addEventListener('mousedown', onAway)
  nextTick(() => { if (search.value) search.value.focus(); clampMenu() })
}
function onAway(e) { if (root.value && !root.value.contains(e.target)) close() }

// Keep the popup within the viewport horizontally.
function clampMenu() {
  const el = menu.value
  if (!el) return
  el.style.left = ''
  let r = el.getBoundingClientRect()
  const over = r.right - (window.innerWidth - 8)
  if (over > 0) el.style.left = -over + 'px'
  r = el.getBoundingClientRect()
  if (r.left < 8) el.style.left = (parseFloat(el.style.left || '0') + (8 - r.left)) + 'px'
}

onBeforeUnmount(() => document.removeEventListener('mousedown', onAway))
</script>
