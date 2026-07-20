<template>
  <Teleport to="body">
  <div class="modal-backdrop" @click.self="close">
    <div ref="root" class="modal settings-modal" role="dialog" aria-modal="true" aria-label="Session options" tabindex="-1">
      <div class="settings-head">
        <h3>Options</h3>
        <button class="modal-x" aria-label="Close" @click="close"><Icon name="x" :size="16" /></button>
      </div>

      <template v-if="modes.length">
        <div class="modal-label">Mode</div>
        <div class="opt-seg">
          <button v-for="o in modes" :key="o.id" :class="{ on: mode === o.id }" @click="$emit('setMode', o.id)">{{ o.label }}</button>
        </div>
      </template>

      <template v-if="efforts.length">
        <div class="modal-label">Effort</div>
        <div class="opt-seg cap">
          <button v-for="o in efforts" :key="o.id" :class="{ on: effortLabel === o.id }" @click="$emit('setEffort', o)">{{ o.label }}</button>
        </div>
      </template>

      <template v-if="models.length">
        <div class="modal-label">Model</div>
        <input v-if="searchable" v-model="q" class="opt-search" placeholder="Search models…" aria-label="Search models" />
        <div class="opt-list">
          <button v-for="o in shownModels" :key="o.id" :class="{ on: modelLabel === o.label }" @click="$emit('setModel', o)">
            <span>{{ o.label }}</span>
            <Icon v-if="modelLabel === o.label" name="check" :size="15" />
          </button>
          <div v-if="searchable && !shownModels.length" class="opt-empty">No matches</div>
        </div>
      </template>
    </div>
  </div>
  </Teleport>
</template>

<script setup>
import { ref, computed } from 'vue'
import { useModal } from '../composables/useModal'
import Icon from './Icon.vue'

const props = defineProps({
  modes: { type: Array, default: () => [] },
  models: { type: Array, default: () => [] },
  efforts: { type: Array, default: () => [] },
  mode: String, modelLabel: String, effortLabel: String,
  searchable: Boolean,
})
const emit = defineEmits(['close', 'setMode', 'setModel', 'setEffort'])

const root = ref(null)
function close() { emit('close') }
useModal(root, close)

const q = ref('')
const shownModels = computed(() => {
  const s = q.value.trim().toLowerCase()
  return s ? props.models.filter((o) => o.label.toLowerCase().includes(s)) : props.models
})
</script>
