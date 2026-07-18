<template>
  <div v-if="m.kind === 'text'" class="row" :class="m.role">
    <div class="bubble" :class="m.role" v-html="renderText(m.text)"></div>
  </div>

  <div v-else-if="m.kind === 'command'" class="cmd-row">
    <span class="cmd">{{ m.text }}</span>
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
import { computed, inject, ref } from 'vue'
import { renderText, firstLine, toolSummary } from '../lib/format'
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
</script>
