<template>
  <VirtualList ref="vlist" tag="main" class="transcript" :items="messages" :item-key="keyOf"
    :estimate="72" stick-bottom @scroll="onScroll">
    <template #before><slot name="top" /><slot name="empty" /></template>
    <template #default="{ item }"><MessageItem :m="item" /></template>
    <template #after><slot name="after" /></template>
  </VirtualList>
  <div class="fab-slot">
    <button v-if="showFab" class="scroll-fab" @click="toBottom" aria-label="Scroll to bottom">
      <Icon name="arrow-down" :size="16" />
    </button>
  </div>
</template>

<script setup>
import { ref, reactive, computed, provide } from 'vue'
import MessageItem from './MessageItem.vue'
import Icon from './Icon.vue'
import VirtualList from './VirtualList.vue'

const props = defineProps({
  messages: { type: Array, default: () => [] },
  hideFab: { type: Boolean, default: false },
})
const emit = defineEmits(['scrolled', 'reachTop'])

const vlist = ref(null)
const atBottom = ref(true)
const showFab = computed(() => !atBottom.value && !props.hideFab)

const keyOf = (m) => m.line + ':' + m.kind + ':' + m.name

// Expanded thinking/tool blocks, kept here (not in the recycled row) so they
// survive the virtualizer unmounting and remounting a message.
provide('openStore', reactive({}))

function onScroll() {
  const el = vlist.value?.viewport
  atBottom.value = vlist.value ? vlist.value.nearBottom() : true
  emit('scrolled', (el?.scrollTop || 0) > 4)
  if (el && el.scrollTop < 400) emit('reachTop')
}
function toBottom() { vlist.value?.scrollToBottom() }
// Sticking to the bottom on new messages is handled by VirtualList; this only
// covers the explicit "jump down after I send" case.
function scrollDown(force) {
  if (force || (vlist.value && vlist.value.nearBottom())) vlist.value?.scrollToBottom()
}

defineExpose({ scrollDown, atBottom })
</script>
