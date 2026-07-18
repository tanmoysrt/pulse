<template>
  <router-view v-slot="{ Component }">
    <component :is="Component" :key="$route.path" />
  </router-view>
</template>

<script setup>
import { onMounted, onBeforeUnmount } from 'vue'

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
