<template>
  <svg class="spark" :viewBox="`0 0 ${W} ${H}`" preserveAspectRatio="none" aria-hidden="true">
    <path v-if="d.area" class="spark-fill" :d="d.area" :style="{ fill: color }" />
    <path v-if="d.line" class="spark-line" :d="d.line" :style="{ stroke: color }" />
  </svg>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  values: { type: Array, default: () => [] },
  // Fixed axis top; when null the sparkline auto-scales to its own peak.
  max: { type: Number, default: null },
  color: { type: String, default: 'currentColor' },
})

const W = 100
const H = 34
const PAD = 3

// Build the line and area paths in the viewBox's coordinate space. A
// non-scaling stroke (CSS) keeps the line crisp despite the stretch to width.
const d = computed(() => {
  const vs = props.values
  if (vs.length < 2) return { line: '', area: '' }
  const top = props.max != null ? props.max : Math.max(...vs, 1)
  const n = vs.length
  const pts = vs.map((v, i) => {
    const x = (i / (n - 1)) * W
    const y = H - PAD - (Math.max(0, Math.min(v, top)) / top) * (H - 2 * PAD)
    return [x, y]
  })
  const line = pts.map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(2)} ${y.toFixed(2)}`).join(' ')
  const area = `${line} L${W} ${H} L0 ${H} Z`
  return { line, area }
})
</script>
