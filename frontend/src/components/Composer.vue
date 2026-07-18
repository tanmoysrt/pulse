<template>
  <footer>
    <div class="composer" :class="{ busy }">
      <div class="attach-strip" v-if="attachments.length">
        <div v-for="a in attachments" :key="a.id" class="attach-chip" :class="{ error: a.status === 'error' }">
          <img v-if="a.previewUrl && isImageType(a.type)" class="thumb" :src="a.previewUrl" alt="" />
          <span v-else class="filesvg">{{ fileExtLabel(a.name) }}</span>
          <span class="attach-name">{{ a.status === 'error' ? 'Failed – tap to remove' : a.name }}</span>
          <span v-if="a.status === 'uploading'" class="attach-spin"></span>
          <button class="attach-remove" title="Remove" @click="removeAttachment(a.id)">✕</button>
        </div>
      </div>

      <textarea ref="input" rows="1" v-model="text" :disabled="disabled"
        :placeholder="disabled ? 'Session closed' : ('Message ' + agentLabel(agent) + '…')"
        @input="autogrow" @keydown="onKeydown"></textarea>

      <div class="composer-bottom">
        <div class="toolbar">
          <Selector v-if="modes.length" :label="modeLabel || 'Mode'" :options="modes"
            :current="(o) => mode === o.id" @select="(o) => $emit('setMode', o.id)" />
          <Selector v-if="models.length" :label="modelLabel || 'Model'" :options="models" :searchable="agent === 'opencode'"
            :current="(o) => modelLabel === o.label" @select="(o) => $emit('setModel', o)" />
          <Selector v-if="efforts.length" cap :label="effortLabel || 'Effort'" :options="efforts"
            :current="(o) => effortLabel === o.id" @select="(o) => $emit('setEffort', o)" />
        </div>

        <button class="attachbtn" title="Attach file" aria-label="Attach file" @click="pickFile">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M21.44 11.05l-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48" />
          </svg>
        </button>

        <button v-if="busy" class="iconbtn stopbtn" title="Stop" aria-label="Stop" @click="$emit('stop')">
          <svg width="12" height="12" viewBox="0 0 14 14"><rect x="2" y="2" width="10" height="10" rx="2" fill="currentColor" /></svg>
        </button>
        <button v-else class="iconbtn send" title="Send" aria-label="Send" :disabled="!canSend" @click="doSend">
          <svg width="14" height="14" viewBox="0 0 18 18"><path d="M2 9L16 2L11 16L8.5 10.5L2 9Z" fill="currentColor" /></svg>
        </button>
      </div>
    </div>
    <input ref="file" type="file" multiple hidden @change="onFilesChosen" />
  </footer>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount, nextTick } from 'vue'
import Selector from './Selector.vue'
import { agentLabel } from '../constants'
import { isImageType, fileExtLabel } from '../lib/format'

const props = defineProps({
  agent: String, busy: Boolean, disabled: Boolean,
  modes: Array, models: Array, efforts: Array,
  mode: String, modelLabel: String, effortLabel: String,
  uploadFn: Function,
})
const emit = defineEmits(['send', 'stop', 'setMode', 'setModel', 'setEffort'])

const text = ref('')
const attachments = ref([])
const sending = ref(false)
let seq = 0

const input = ref(null)
const file = ref(null)
const footer = ref(null)

const modeLabel = computed(() => (props.modes.find((m) => m.id === props.mode) || {}).label || '')
const ready = computed(() => attachments.value.filter((a) => a.status === 'done'))
const uploading = computed(() => attachments.value.some((a) => a.status === 'uploading'))
const canSend = computed(() => !props.disabled && !sending.value && !uploading.value && (!!text.value.trim() || ready.value.length > 0))

function autogrow() {
  const el = input.value
  el.style.height = 'auto'
  const vh = window.visualViewport ? window.visualViewport.height : window.innerHeight
  el.style.height = Math.min(el.scrollHeight, vh * 0.35) + 'px'
}
function onKeydown(e) {
  if (e.key === 'Enter' && !e.shiftKey && !e.ctrlKey && !e.metaKey && !e.altKey && !e.isComposing) {
    e.preventDefault(); doSend()
  }
}
function pickFile() { file.value.click() }
function onFilesChosen(e) {
  Array.from(e.target.files).forEach((f) => {
    seq++
    const a = { id: seq, name: f.name, type: f.type, status: 'uploading', path: null, previewUrl: isImageType(f.type) ? URL.createObjectURL(f) : null }
    attachments.value.push(a)
    props.uploadFn(f).then((d) => { a.status = 'done'; a.path = d.path }).catch(() => { a.status = 'error' })
  })
  e.target.value = ''
}
function removeAttachment(id) {
  const i = attachments.value.findIndex((a) => a.id === id)
  if (i < 0) return
  if (attachments.value[i].previewUrl) URL.revokeObjectURL(attachments.value[i].previewUrl)
  attachments.value.splice(i, 1)
}
function doSend() {
  if (!canSend.value) return
  sending.value = true
  const caption = text.value.trim()
  const paths = ready.value.map((a) => a.path).join('\n')
  const full = paths && caption ? paths + '\n' + caption : (paths || caption)
  const snapshot = ready.value.map((a) => ({ name: a.name, type: a.type, previewUrl: a.previewUrl }))
  emit('send', { full, caption, snapshot }, (ok) => {
    sending.value = false
    if (ok) {
      attachments.value = attachments.value.filter((a) => a.status !== 'done')
      text.value = ''
      nextTick(autogrow)
    }
  })
}

let ro = null
onMounted(() => {
  footer.value = input.value.closest('footer')
  if (window.ResizeObserver && footer.value) {
    ro = new ResizeObserver((entries) => {
      document.documentElement.style.setProperty('--footer-h', entries[0].contentRect.height + 'px')
    })
    ro.observe(footer.value)
  }
})
onBeforeUnmount(() => { if (ro) ro.disconnect() })
defineExpose({ focus: () => input.value && input.value.focus() })
</script>
