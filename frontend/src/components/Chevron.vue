<template>
  <svg class="chev" :class="{ up }" width="10" height="10" viewBox="0 0 10 10">
    <path d="M2 3.5L5 6.5L8 3.5" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linecap="round" stroke-linejoin="round" />
  </svg>
</template>

<script setup>
defineProps({ up: Boolean })
</script>
