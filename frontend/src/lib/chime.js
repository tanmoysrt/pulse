// A short two-note chime, synthesized so there's no audio asset to ship or
// fetch. Browsers won't play a custom sound for OS-level push notifications
// (the Notification API dropped that field years ago) — this only plays
// while the app itself is open, via the service worker's 'pulse-chime' message.
let ctx = null

export function playChime() {
  try {
    ctx = ctx || new (window.AudioContext || window.webkitAudioContext)()
    if (ctx.state === 'suspended') ctx.resume()
    const now = ctx.currentTime
    for (const [freq, delay] of [[880, 0], [1318.51, 0.11]]) {
      const osc = ctx.createOscillator()
      const gain = ctx.createGain()
      osc.type = 'sine'
      osc.frequency.value = freq
      gain.gain.setValueAtTime(0, now + delay)
      gain.gain.linearRampToValueAtTime(0.25, now + delay + 0.015)
      gain.gain.exponentialRampToValueAtTime(0.0001, now + delay + 0.35)
      osc.connect(gain).connect(ctx.destination)
      osc.start(now + delay)
      osc.stop(now + delay + 0.4)
    }
  } catch (e) { /* audio unavailable — not worth surfacing */ }
}
