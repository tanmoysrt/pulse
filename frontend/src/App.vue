<template>
  <router-view v-slot="{ Component }">
    <component :is="Component" :key="$route.path" />
  </router-view>
</template>

<script setup>
import { onMounted, onBeforeUnmount } from 'vue'

// The bootstrap token (?t=) is already consumed server-side by now; drop it from the visible URL.
const params = new URLSearchParams(window.location.search)
if (params.has('t')) {
  params.delete('t')
  const qs = params.toString()
  history.replaceState(null, '', window.location.pathname + (qs ? '?' + qs : '') + window.location.hash)
}

// Expose keyboard height as --kb-offset for fixed composer/sheets.
const vv = window.visualViewport
function updateKeyboardOffset() {
  if (!vv) return
  const offset = Math.max(0, Math.round(window.innerHeight - vv.height - vv.offsetTop))
  document.documentElement.style.setProperty('--kb-offset', offset + 'px')
}

onMounted(() => {
  if (vv) { vv.addEventListener('resize', updateKeyboardOffset); vv.addEventListener('scroll', updateKeyboardOffset) }
  updateKeyboardOffset()
})
onBeforeUnmount(() => {
  if (vv) { vv.removeEventListener('resize', updateKeyboardOffset); vv.removeEventListener('scroll', updateKeyboardOffset) }
})
</script>
