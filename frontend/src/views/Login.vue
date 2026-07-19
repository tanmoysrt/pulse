<template>
  <div class="login">
    <div class="login-card">
      <span class="login-logo">P</span>
      <h1 class="login-title">Pulse</h1>
      <p class="login-sub">Enter your password to continue</p>

      <form class="login-form" @submit.prevent="submit">
        <input
          ref="pw" v-model="password" type="password" autocomplete="current-password"
          placeholder="Password" :disabled="locked" aria-label="Password"
        />
        <button type="submit" class="btn btn-primary btn-block" :disabled="locked || !password || busy">
          {{ busy ? 'Checking…' : 'Unlock' }}
        </button>
      </form>

      <p v-if="msg" class="login-msg" :class="{ err: isErr }">{{ msg }}</p>
      <p class="login-hint">Tip: scanning the QR from the terminal skips this.</p>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { login } from '../lib/api'

const router = useRouter()
const password = ref('')
const busy = ref(false)
const locked = ref(false)
const isErr = ref(false)
const msg = ref('')
const pw = ref(null)
let timer = null

function fmt(secs) {
  const m = Math.floor(secs / 60)
  const s = secs % 60
  return m > 0 ? `${m}m ${s}s` : `${s}s`
}

function lockFor(secs) {
  locked.value = true
  isErr.value = true
  const tick = () => {
    msg.value = `Too many attempts. Try again in ${fmt(secs)}.`
    if (secs <= 0) { clearInterval(timer); locked.value = false; msg.value = ''; nextTick(() => pw.value && pw.value.focus()) }
    secs--
  }
  tick()
  timer = setInterval(tick, 1000)
}

async function submit() {
  if (busy.value || locked.value) return
  busy.value = true
  msg.value = ''
  const r = await login(password.value)
  busy.value = false
  if (r.ok) {
    const after = sessionStorage.getItem('pulse.after') || '/'
    sessionStorage.removeItem('pulse.after')
    router.replace(after)
    return
  }
  if (r.status === 429) { lockFor(r.retryAfter || 900); return }
  isErr.value = true
  msg.value = 'Incorrect password. Try again.'
  password.value = ''
  nextTick(() => pw.value && pw.value.focus())
}

onMounted(() => nextTick(() => pw.value && pw.value.focus()))
onBeforeUnmount(() => clearInterval(timer))
</script>
