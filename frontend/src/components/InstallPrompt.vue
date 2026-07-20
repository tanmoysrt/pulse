<template>
  <Teleport to="body">
    <div class="notify-prompt">
      <div class="np-inner">
        <span class="np-icon"><PulseLogo class="np-logo" /></span>
        <h2 class="np-title">Add Pulse to your home screen</h2>
        <p v-if="ios" class="np-text">Tap the Share button in Safari's toolbar, then "Add to Home Screen".</p>
        <p v-else class="np-text">Install Pulse for quick access and a full-screen app, no browser bar.</p>
        <div class="np-actions">
          <button v-if="!ios" class="btn btn-primary btn-block" @click="$emit('install')">Install</button>
          <button class="np-skip" @click="$emit('dismiss')">{{ ios ? 'Got it' : 'Not now' }}</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup>
import PulseLogo from './PulseLogo.vue'
defineProps({ ios: Boolean })
defineEmits(['install', 'dismiss'])
</script>
