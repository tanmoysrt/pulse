<template>
  <main ref="scroller" @scroll="onScroll">
    <slot name="empty" />
    <div class="messages">
      <MessageItem v-for="m in messages" :key="m.line + ':' + m.kind + ':' + m.name" :m="m" />
    </div>
    <slot name="after" />
  </main>
  <div class="fab-slot">
    <button v-if="showFab" class="scroll-fab" @click="scrollDown(true)" aria-label="Scroll to bottom">
      <svg width="14" height="14" viewBox="0 0 18 18"><path d="M4 7L9 12L14 7" stroke="currentColor" stroke-width="1.8" fill="none" stroke-linecap="round" stroke-linejoin="round" /></svg>
    </button>
  </div>
</template>

<script setup>
import { ref, watch, nextTick, computed } from 'vue'
import MessageItem from './MessageItem.vue'

const props = defineProps({
  messages: { type: Array, default: () => [] },
  hideFab: { type: Boolean, default: false },
})
const emit = defineEmits(['scrolled'])

const scroller = ref(null)
const atBottom = ref(true)
const scrolled = ref(false)

const showFab = computed(() => !atBottom.value && !props.hideFab)

function nearBottom() {
  const el = scroller.value
  return !el || el.scrollHeight - el.scrollTop - el.clientHeight < 120
}
function scrollDown(force) {
  if (!force && !nearBottom()) return
  nextTick(() => { const el = scroller.value; if (el) el.scrollTop = el.scrollHeight })
}
function onScroll() {
  const el = scroller.value
  atBottom.value = nearBottom()
  scrolled.value = (el?.scrollTop || 0) > 4
  emit('scrolled', scrolled.value)
}

// Follow new messages only when the reader is already at the bottom.
watch(() => props.messages.length, () => {
  const stick = nearBottom()
  nextTick(() => { if (stick) scrollDown(true) })
})

defineExpose({ scrollDown, atBottom })
</script>
