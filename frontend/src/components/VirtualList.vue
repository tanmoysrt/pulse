<template>
  <component :is="tag" ref="viewport" class="vlist" @scroll="onScroll">
    <div ref="content">
      <slot name="before" />
      <div :style="{ height: padTop + 'px' }" aria-hidden="true"></div>
      <div v-for="row in windowRows" :key="row.key" class="vrow" :ref="(el) => setRow(row.key, el)">
        <slot :item="row.item" :index="row.index" />
      </div>
      <div :style="{ height: padBottom + 'px' }" aria-hidden="true"></div>
      <slot name="after" />
    </div>
  </component>
</template>

<script setup>
// Dynamic-height windowing: only rows near the viewport are mounted; measured
// heights feed the layout and the first visible row is held steady on changes.
import { ref, computed, shallowRef, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'

const props = defineProps({
  items: { type: Array, default: () => [] },
  itemKey: { type: Function, required: true },
  estimate: { type: Number, default: 64 },
  overscan: { type: Number, default: 800 },
  tag: { type: String, default: 'div' },
  stickBottom: { type: Boolean, default: false },
})
const emit = defineEmits(['scroll'])

const viewport = ref(null)
const content = ref(null)
const scrollTop = ref(0)
const viewportH = ref(0)
const prefix = shallowRef([0]) // prefix[i] = top edge of item i
const rowEls = new Map()
const sizes = new Map()
let stick = props.stickBottom
let prevFirstKey = null

// Unmeasured rows use the running average of measured ones.
let sizeSum = 0, sizeCount = 0
function record(key, ht) {
  if (sizes.has(key)) sizeSum -= sizes.get(key)
  else sizeCount++
  sizeSum += ht
  sizes.set(key, ht)
}
const avg = () => (sizeCount ? sizeSum / sizeCount : props.estimate)
const h = (i) => sizes.get(props.itemKey(props.items[i])) ?? avg()

function rebuild() {
  const n = props.items.length
  const pf = new Array(n + 1)
  pf[0] = 0
  for (let i = 0; i < n; i++) pf[i + 1] = pf[i] + h(i)
  prefix.value = pf
}

function captureAnchor() {
  const el = viewport.value
  const vpTop = el.getBoundingClientRect().top
  for (const [k, node] of rowEls) {
    const r = node.getBoundingClientRect()
    if (r.bottom > vpTop + 1) return { k, top: r.top }
  }
  return null
}
function restoreAnchor(a) {
  if (!a) return
  const el = viewport.value, node = rowEls.get(a.k)
  if (node) {
    const delta = node.getBoundingClientRect().top - a.top
    if (delta) { el.scrollTop += delta; scrollTop.value = el.scrollTop }
  }
}

// largest i with pf[i] <= y
function bisect(pf, y) {
  let lo = 0, hi = pf.length - 1
  while (lo < hi) { const mid = (lo + hi + 1) >> 1; if (pf[mid] <= y) lo = mid; else hi = mid - 1 }
  return lo
}

const range = computed(() => {
  const pf = prefix.value, n = props.items.length
  if (!n) return { start: 0, end: -1 }
  const start = Math.max(0, bisect(pf, scrollTop.value - props.overscan))
  const end = Math.min(n - 1, bisect(pf, scrollTop.value + viewportH.value + props.overscan))
  return { start, end }
})
const windowRows = computed(() => {
  const { start, end } = range.value, out = []
  for (let i = start; i <= end; i++) out.push({ key: props.itemKey(props.items[i]), item: props.items[i], index: i })
  return out
})
const padTop = computed(() => prefix.value[range.value.start] || 0)
const padBottom = computed(() => {
  const pf = prefix.value, total = pf[pf.length - 1]
  return Math.max(0, total - (pf[range.value.end + 1] ?? total))
})

function setRow(key, el) { if (el) rowEls.set(key, el); else rowEls.delete(key) }

let raf = 0
const schedule = () => { if (!raf) raf = requestAnimationFrame(measure) }
function measure() {
  raf = 0
  const el = viewport.value
  if (!el) return
  let changed = false
  for (const [key, node] of rowEls) {
    const ht = node.offsetHeight
    if (ht && sizes.get(key) !== ht) { record(key, ht); changed = true }
  }
  if (stick) {
    rebuild()
    nextTick(() => { const v = viewport.value; if (v) v.scrollTop = v.scrollHeight })
    return
  }
  if (!changed) return
  const a = captureAnchor()
  rebuild()
  nextTick(() => restoreAnchor(a))
}

function onScroll() {
  const el = viewport.value; if (!el) return
  scrollTop.value = el.scrollTop
  stick = el.scrollHeight - el.scrollTop - el.clientHeight < 40
  emit('scroll')
}

let ro
onMounted(() => {
  rebuild()
  prevFirstKey = props.items.length ? props.itemKey(props.items[0]) : null
  viewportH.value = viewport.value.clientHeight
  ro = new ResizeObserver(() => {
    if (viewport.value) viewportH.value = viewport.value.clientHeight
    schedule()
  })
  ro.observe(viewport.value)
  ro.observe(content.value)
  if (stick) nextTick(scrollToBottom)
})
onBeforeUnmount(() => { if (ro) ro.disconnect(); if (raf) cancelAnimationFrame(raf) })

watch(() => props.items.length, (n, old) => {
  const firstKey = n ? props.itemKey(props.items[0]) : null
  const prepended = n > old && firstKey !== prevFirstKey
  prevFirstKey = firstKey
  if (prepended && !stick) {
    const el = viewport.value
    const vpTop = el.getBoundingClientRect().top
    let aKey = null, aOffset = 0
    for (const [k, node] of rowEls) {
      const r = node.getBoundingClientRect()
      if (r.bottom > vpTop + 1) { aKey = k; aOffset = r.top - vpTop; break }
    }
    rebuild()
    nextTick(() => {
      const idx = props.items.findIndex((it) => props.itemKey(it) === aKey)
      if (idx >= 0) { el.scrollTop = prefix.value[idx] - aOffset; scrollTop.value = el.scrollTop }
      schedule()
    })
  } else {
    rebuild(); schedule()
  }
})

function scrollToBottom() {
  const el = viewport.value; if (!el) return
  stick = true
  nextTick(() => { el.scrollTop = el.scrollHeight; scrollTop.value = el.scrollTop })
}
function nearBottom(px = 120) {
  const el = viewport.value
  return !el || el.scrollHeight - el.scrollTop - el.clientHeight < px
}

defineExpose({ viewport, scrollToBottom, nearBottom })
</script>

<style scoped>
.vlist { overflow-y: auto; }
.vrow { display: flow-root; } /* contain child margins so measured height is exact */
</style>
