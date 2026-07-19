<template>
  <Teleport to="body">
    <div class="notify-prompt">
      <div class="np-inner">
        <span class="np-icon"><Icon name="bell" :size="30" /></span>
        <h2 class="np-title">Stay in the loop</h2>
        <p class="np-text">Get a notification the moment an agent finishes or needs your approval — even when Pulse isn't open.</p>
        <div class="np-actions">
          <button class="np-enable" @click="$emit('enable')">Enable notifications</button>
          <button class="np-skip" @click="$emit('dismiss')">Not now</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup>
import Icon from './Icon.vue'
defineEmits(['enable', 'dismiss'])
</script>
