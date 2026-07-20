<template>
  <footer>
    <div class="composer" :class="{ busy }">
      <div class="attach-strip" v-if="attachments.length">
        <div v-for="a in attachments" :key="a.id" class="attach-chip" :class="{ error: a.status === 'error' }">
          <img v-if="a.previewUrl && isImageType(a.type)" class="thumb" :src="a.previewUrl" alt="" />
          <span v-else class="filesvg">{{ fileExtLabel(a.name) }}</span>
          <span class="attach-name">{{ a.status === 'error' ? 'Failed – tap to remove' : a.name }}</span>
          <span v-if="a.status === 'uploading'" class="attach-spin"></span>
          <button class="attach-remove" title="Remove attachment" aria-label="Remove attachment" @click="removeAttachment(a.id)">
            <Icon name="x" :size="12" />
          </button>
        </div>
      </div>

      <textarea ref="input" rows="1" :value="text" :disabled="disabled"
        :placeholder="disabled ? 'Session closed' : ('Message ' + agentLabel(agent) + '…')"
        :aria-label="disabled ? 'Session closed' : ('Message ' + agentLabel(agent))"
        @input="onInput" @keydown="onKeydown"></textarea>

      <div class="composer-bottom">
        <div class="toolbar">
          <button v-if="hasOptions" class="opts-btn" title="Session options" @click="optsOpen = true">
            <Icon name="sliders" :size="14" />
            <span class="opts-label">{{ modeLabel || 'Options' }}</span>
          </button>
        </div>

        <button class="attachbtn" title="Attach file" aria-label="Attach file" @click="pickFile">
          <Icon name="paperclip" :size="15" />
        </button>

        <button v-if="busy" class="iconbtn stopbtn" title="Stop" aria-label="Stop" @click="$emit('stop')">
          <Icon name="square" :size="13" />
        </button>
        <button v-else class="iconbtn send" title="Send" aria-label="Send" :disabled="!canSend" @click="doSend">
          <Icon name="send" :size="15" />
        </button>
      </div>
    </div>
    <input ref="file" type="file" multiple hidden @change="onFilesChosen" />

    <SessionSettingsModal v-if="optsOpen"
      :modes="modes" :models="models" :efforts="efforts" :searchable="agent === 'opencode'"
      :mode="mode" :model-label="modelLabel" :effort-label="effortLabel"
      @close="optsOpen = false"
      @set-mode="(v) => $emit('setMode', v)" @set-model="(v) => $emit('setModel', v)" @set-effort="(v) => $emit('setEffort', v)" />
  </footer>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount, nextTick } from 'vue'
import SessionSettingsModal from './SessionSettingsModal.vue'
import Icon from './Icon.vue'
import { agentLabel } from '../constants'
import { isImageType, fileExtLabel } from '../lib/format'

const props = defineProps({
  agent: String, busy: Boolean, disabled: Boolean,
  modes: Array, models: Array, efforts: Array,
  mode: String, modelLabel: String, effortLabel: String,
  uploadFn: Function,
})
const emit = defineEmits(['send', 'stop', 'setMode', 'setModel', 'setEffort', 'resize'])

const text = ref('')
const attachments = ref([])
const optsOpen = ref(false)
let seq = 0

const input = ref(null)
const file = ref(null)
const footer = ref(null)

const modeLabel = computed(() => (props.modes.find((m) => m.id === props.mode) || {}).label || '')
const hasOptions = computed(() => props.modes.length > 0 || props.models.length > 0 || props.efforts.length > 0)
const ready = computed(() => attachments.value.filter((a) => a.status === 'done'))
const uploading = computed(() => attachments.value.some((a) => a.status === 'uploading'))
// Enabled the moment there's something to send. We bind :value/@input (not
// v-model) so a keystroke registers immediately, even mid IME composition where
// v-model would defer its update until the field loses focus.
const canSend = computed(() => !props.disabled && !uploading.value && (!!text.value.trim() || ready.value.length > 0))

function onInput(e) {
  text.value = e.target.value
  autogrow()
}
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
  const caption = text.value.trim()
  const paths = ready.value.map((a) => a.path).join('\n')
  const full = paths && caption ? paths + '\n' + caption : (paths || caption)
  const snapshot = ready.value.map((a) => ({ name: a.name, type: a.type, previewUrl: a.previewUrl }))

  // Clear right away so sending never feels like it blocks; put the text and
  // attachments back if the send is rejected.
  const prevText = text.value
  const prevAttachments = attachments.value.slice()
  attachments.value = attachments.value.filter((a) => a.status !== 'done')
  text.value = ''
  nextTick(autogrow)

  emit('send', { full, caption, snapshot }, (ok) => {
    if (ok) return
    text.value = prevText
    attachments.value = prevAttachments
    nextTick(autogrow)
  })
}

let ro = null
onMounted(() => {
  footer.value = input.value.closest('footer')
  if (window.ResizeObserver && footer.value) {
    ro = new ResizeObserver((entries) => {
      // The transcript's scrollable area is padded by this var, so its
      // scrollHeight changes without the transcript's own ResizeObserver
      // ever seeing it (the footer isn't inside it) — nudge it back to
      // bottom explicitly, e.g. once the real height replaces the CSS
      // fallback on first mount, or as the composer grows while typing.
      document.documentElement.style.setProperty('--footer-h', entries[0].contentRect.height + 'px')
      emit('resize')
    })
    ro.observe(footer.value)
  }
})
onBeforeUnmount(() => { if (ro) ro.disconnect() })
defineExpose({ focus: () => input.value && input.value.focus() })
</script>
