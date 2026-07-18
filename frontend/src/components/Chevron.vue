<template>
  <Icon name="chevron-down" class="chev" :class="{ up }" :size="14" />
</template>

<script setup>
import Icon from './Icon.vue'
defineProps({ up: Boolean })
</script>
