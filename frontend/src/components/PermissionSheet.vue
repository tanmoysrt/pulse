<template>
  <div>
    <div class="perm-backdrop"></div>
    <div ref="root" class="perm-sheet" role="dialog" aria-modal="true" :aria-label="permAction(pending)" tabindex="-1">
      <div class="perm-handle"></div>
      <div class="perm-top">
        <div class="perm-title-row"><span class="perm-action">{{ permAction(pending) }}</span></div>
        <button class="perm-more" @click="details = !details">{{ details ? 'Less' : 'Details' }}</button>
      </div>
      <span class="perm-sum">{{ permSummary(pending) }}</span>
      <pre v-if="details" class="perm-details mono">{{ permDetails(pending) }}</pre>
      <div class="perm-actions">
        <button class="allow" @click="$emit('decide', 'allow')">Approve</button>
        <button class="deny" @click="$emit('decide', 'deny')">Deny</button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, watch } from 'vue'
import { permAction, permSummary, permDetails } from '../lib/format'
import { useModal } from '../composables/useModal'

const props = defineProps({ pending: { type: Object, required: true } })
const emit = defineEmits(['decide'])
const details = ref(false)
watch(() => props.pending && props.pending.id, () => { details.value = false })

// Escape denies rather than just closing, the safer default here.
const root = ref(null)
useModal(root, () => emit('decide', 'deny'))
</script>
