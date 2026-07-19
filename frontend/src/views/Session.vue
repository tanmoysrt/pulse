<template>
  <div class="app">
    <div v-if="!appReady" class="boot-loader"><div class="pulse-rings"><span></span><span></span><span></span></div></div>

    <SessionHeader
      :title="state.title" :status="state.status" :todos="state.todos" :tasksOpen="tasksOpen"
      :shadow="shadow"
      @back="goHome" @toggleTasks="tasksOpen = !tasksOpen" @clear="onClear" @compact="compact" @close="onClose" />

    <TasksSheet v-if="tasksOpen && state.todos.length" :todos="state.todos" />

    <Transcript ref="transcript" :messages="state.messages" :hide-fab="!!state.pending" @scrolled="shadow = $event">
      <template #empty>
        <div v-if="showEmpty" class="empty">
          <div class="pulse-rings"><span></span><span></span><span></span></div>
          <h2>{{ state.status === 'connecting' ? 'Connecting…' : 'Ready when you are' }}</h2>
          <p>{{ state.status === 'connecting' ? ('Linking up with ' + agentLabel(state.agent) + '.') : 'Send a message below to get started.' }}</p>
        </div>
      </template>
      <template #after>
        <div class="queued">
          <div v-for="(q, i) in state.queued" :key="i" class="row user">
            <div class="bubble user pending" title="Queued">
              <div v-if="q.attachments && q.attachments.length" class="bubble-attachments">
                <template v-for="(a, j) in q.attachments" :key="j">
                  <img v-if="a.previewUrl && isImageType(a.type)" class="thumb" :src="a.previewUrl" alt="" />
                  <span v-else class="filesvg">{{ fileExtLabel(a.name) }}</span>
                </template>
              </div>
              <div v-if="q.caption">{{ q.caption }}</div>
            </div>
          </div>
        </div>
        <div v-if="state.status === 'running'" class="working">
          <span class="pulse-dot"></span>
          <span class="working-verb">{{ workingVerb }}…</span>
          <span class="working-time">{{ elapsed }}s</span>
        </div>
      </template>
    </Transcript>

    <PermissionSheet v-if="state.pending" :pending="state.pending" @decide="onDecide" />

    <Composer ref="composer"
      :agent="state.agent" :busy="isBusy" :disabled="state.closed"
      :modes="modes" :models="models" :efforts="efforts"
      :mode="state.mode" :model-label="state.modelLabel" :effort-label="state.effortLabel" :upload-fn="uploadFile"
      @send="onSend" @stop="interrupt" @set-mode="setMode" @set-model="setModel" @set-effort="setEffort" />
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useSession } from '../composables/useSession'
import { agentLabel } from '../constants'
import { isImageType, fileExtLabel } from '../lib/format'
import SessionHeader from '../components/SessionHeader.vue'
import TasksSheet from '../components/TasksSheet.vue'
import Transcript from '../components/Transcript.vue'
import PermissionSheet from '../components/PermissionSheet.vue'
import Composer from '../components/Composer.vue'

const route = useRoute()
const router = useRouter()
const id = route.params.id

const {
  state, elapsed, isBusy, modes, models, efforts, workingVerb, appReady,
  connect, send, interrupt, decide, clear, compact, close, setMode, setModel, setEffort, uploadFile,
} = useSession(id, route.query.a || '', route.query.t || '')

const transcript = ref(null)
const composer = ref(null)
const tasksOpen = ref(false)
const shadow = ref(false)

const showEmpty = computed(() => !state.closed && !state.messages.length && !isBusy.value)

function goHome() { router.push('/') }
function onClear() { clear() }
function onClose() {
  if (!window.confirm('Close this session? This ends the ' + agentLabel(state.agent) + ' process.')) return
  close().then((r) => { if (r.ok) goHome() })
}
function onDecide(decision) { if (state.pending) decide(state.pending.id, decision) }

function onSend({ full, caption, snapshot }, done) {
  send(full).then((res) => {
    if (res.ok) {
      state.queued.push({ caption, attachments: snapshot })
      nextTick(() => transcript.value && transcript.value.scrollDown(true))
    }
    done(res.ok)
  }).catch(() => done(false))
}

// Leaving the session (closed by daemon) returns home.
watch(() => state.closed, (c) => { if (c) goHome() })

onMounted(() => {
  connect()
  if (window.innerWidth > 640 && !(window.matchMedia && window.matchMedia('(pointer: coarse)').matches)) {
    nextTick(() => composer.value && composer.value.focus())
  }
})
</script>
