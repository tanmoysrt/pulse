import { ref, computed, onMounted, onBeforeUnmount } from 'vue'

const THRESHOLD = 64
const MAX_PULL = 100
const DAMPING = 0.55

// Touch-driven pull-to-refresh, delegated from a stable container ref down to
// whichever scrollable child matching scrollSelector is currently rendered
// inside it (resolved fresh per gesture via the touch target, since the
// child can come and go as content loads/empties). Only engages when that
// child is already scrolled to the top, so a normal downward scroll is never
// intercepted.
export function usePullToRefresh(containerRef, onRefresh, scrollSelector) {
  const distance = ref(0)
  const refreshing = ref(false)
  const ready = computed(() => distance.value >= THRESHOLD)

  let startY = null
  let scrollEl = null
  let dragging = false

  function onTouchStart(e) {
    if (refreshing.value) return
    const el = e.target.closest(scrollSelector)
    if (!el || el.scrollTop > 0) return
    scrollEl = el
    startY = e.touches[0].clientY
    dragging = false
  }
  function onTouchMove(e) {
    if (startY == null) return
    const dy = e.touches[0].clientY - startY
    if (dy <= 0 || (scrollEl && scrollEl.scrollTop > 0)) {
      startY = null
      dragging = false
      distance.value = 0
      return
    }
    dragging = true
    e.preventDefault()
    distance.value = Math.min(MAX_PULL, dy * DAMPING)
  }
  async function onTouchEnd() {
    if (!dragging) { startY = null; return }
    dragging = false
    startY = null
    if (ready.value) {
      refreshing.value = true
      distance.value = THRESHOLD
      try { await onRefresh() } finally {
        refreshing.value = false
        distance.value = 0
      }
    } else {
      distance.value = 0
    }
  }

  onMounted(() => {
    const el = containerRef.value
    if (!el) return
    el.addEventListener('touchstart', onTouchStart, { passive: true })
    el.addEventListener('touchmove', onTouchMove, { passive: false })
    el.addEventListener('touchend', onTouchEnd, { passive: true })
    el.addEventListener('touchcancel', onTouchEnd, { passive: true })
  })
  onBeforeUnmount(() => {
    const el = containerRef.value
    if (!el) return
    el.removeEventListener('touchstart', onTouchStart)
    el.removeEventListener('touchmove', onTouchMove)
    el.removeEventListener('touchend', onTouchEnd)
    el.removeEventListener('touchcancel', onTouchEnd)
  })

  return { distance, refreshing, ready, threshold: THRESHOLD }
}
