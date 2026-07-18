<template>
  <div class="app readonly">
    <div v-if="loading" class="boot-loader"><div class="pulse-rings"><span></span><span></span><span></span></div></div>

    <SessionHeader :title="title" :readonly="true" status="idle" :shadow="shadow" resumable @back="goHome" @resume="onResume" />

    <Transcript :messages="messages" @scrolled="shadow = $event">
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
const title = ref(route.query.t || 'Transcript')
const loading = ref(true)
const shadow = ref(false)

function goHome() { router.push('/') }

async function onResume() {
  try {
    const d = await resumeSession(route.params.ref)
    router.push({ path: '/s/' + d.id, query: { a: d.agent } })
  } catch (e) { window.alert('Could not resume: ' + e.message) }
}

onMounted(async () => {
  try {
    const d = await readHistory(route.params.ref)
    messages.value = d.messages || []
  } catch (e) { /* show empty */ }
  loading.value = false
})
</script>
