<template>
  <div class="app readonly">
    <div v-if="loading" class="boot-loader"><div class="pulse-rings"><span></span><span></span><span></span></div></div>

    <SessionHeader :title="title" :readonly="true" status="idle" :shadow="shadow" resumable @back="goHome" @resume="onResume" />

    <Transcript :messages="messages" @scrolled="shadow = $event" @reachTop="loadMore">
      <template #top>
        <div v-if="loadingMore" class="load-earlier"><span class="think-dot"></span> Loading earlier messages…</div>
      </template>
      <template #empty>
        <div v-if="!loading && !messages.length" class="empty">
          <h2>Empty transcript</h2>
          <p>Nothing to show for this session.</p>
        </div>
      </template>
    </Transcript>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { readHistory, resumeSession } from '../lib/api'
import SessionHeader from '../components/SessionHeader.vue'
import Transcript from '../components/Transcript.vue'

const route = useRoute()
const router = useRouter()

const messages = ref([])
const oldest = ref(0)
const title = ref(route.query.t || 'Transcript')
const loading = ref(true)
const loadingMore = ref(false)
const shadow = ref(false)

// See Session.vue's goHome: pop real history instead of always pushing a
// fresh Home entry, so the back stack doesn't grow unbounded.
function goHome() {
  if (window.history.state && window.history.state.back) router.back()
  else router.push('/')
}

async function onResume() {
  try {
    const d = await resumeSession(route.params.ref)
    router.push({ path: '/s/' + d.id, query: { a: d.agent } })
  } catch (e) { window.alert('Could not resume: ' + e.message) }
}

async function loadMore() {
  if (loadingMore.value || loading.value || oldest.value <= 0) return
  loadingMore.value = true
  try {
    const d = await readHistory(route.params.ref, oldest.value)
    const older = d.messages || []
    if (older.length) messages.value = older.concat(messages.value)
    oldest.value = d.start || 0
  } catch (e) { /* keep what we have */ }
  loadingMore.value = false
}

onMounted(async () => {
  try {
    const d = await readHistory(route.params.ref)
    messages.value = d.messages || []
    oldest.value = d.start || 0
  } catch (e) { /* show empty */ }
  loading.value = false
})
</script>
