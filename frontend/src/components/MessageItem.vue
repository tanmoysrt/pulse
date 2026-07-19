<template>
  <div v-if="m.kind === 'text'" class="row" :class="m.role">
    <div class="bubble-wrap" :class="{ clipped: clip && !open }">
      <div ref="bubbleEl" class="bubble" :class="m.role" v-html="renderText(m.text)"></div>
      <button v-if="clip" class="more-btn" @click="toggle">{{ open ? 'Show less' : 'Show more' }}</button>
    </div>
  </div>

  <div v-else-if="m.kind === 'command'" class="cmd-row">
    <span class="cmd">{{ commandLabel(m.text) }}</span>
  </div>

  <div v-else-if="m.kind === 'tool_group'" class="act-group" :class="{ open }">
    <button class="act-head" @click="toggle">
      <span class="act-icon tool_use"></span>
      <span class="act-name">{{ toolCount }} {{ toolCount === 1 ? 'tool call' : 'tool calls' }}</span>
      <span class="act-sum"></span>
      <Chevron />
    </button>
    <div class="act-group-body">
      <MessageItem v-for="it in m.items" :key="it.line" :m="it" />
    </div>
  </div>

  <div v-else-if="m.kind === 'thinking'" class="thinking" :class="{ open }">
    <button class="think-bar" @click="toggle">
      <span class="think-dot"></span>
      <span class="tag">Thinking</span>
      <span class="think-sum">{{ firstLine(m.text) }}</span>
      <Chevron />
    </button>
    <div class="think-body">{{ m.text }}</div>
  </div>

  <div v-else class="act" :class="{ open }">
    <button class="act-head" @click="toggle">
      <span class="act-icon" :class="m.kind"></span>
      <span class="act-name">{{ m.kind === 'tool_use' ? (m.name || '') : 'Output' }}</span>
      <span class="act-sum">{{ summary }}</span>
      <Chevron />
    </button>
    <pre class="act-body mono">{{ m.text }}</pre>
  </div>
</template>

<script setup>
// Expanded/collapsed state lives in a store the parent provides (keyed by
// message) so it survives the virtualizer recycling this row off-screen.
import { computed, inject, ref, onMounted, nextTick } from 'vue'
import { renderText, firstLine, toolSummary, commandLabel } from '../lib/format'
import Chevron from './Chevron.vue'

const props = defineProps({ m: { type: Object, required: true } })
const store = inject('openStore', null)
const local = ref(false)
const key = () => props.m.line + ':' + props.m.kind + ':' + props.m.name
const open = computed(() => (store ? !!store[key()] : local.value))
function toggle() {
  if (store) store[key()] = !store[key()]
  else local.value = !local.value
}
const summary = computed(() =>
  props.m.kind === 'tool_use' ? toolSummary(props.m) : firstLine(props.m.text))

// Count the tool_use calls in a folded run (results are the paired replies, not
// separate actions), e.g. "3 tool calls".
const toolCount = computed(() => {
  const n = props.m.items.filter((i) => i.kind === 'tool_use').length
  return n || props.m.items.length
})

// Clamp long assistant replies to ~6 lines behind a fade; measured after mount
// (re-runs when the virtualizer remounts the row) so only overflowing text gets
// the "Show more" affordance.
const CLIP_PX = 150
const bubbleEl = ref(null)
const clip = ref(false)
onMounted(() => nextTick(() => {
  if (props.m.role === 'assistant' && bubbleEl.value) clip.value = bubbleEl.value.scrollHeight > CLIP_PX + 24
}))
</script>
