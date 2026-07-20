import { onMounted, onBeforeUnmount, nextTick } from 'vue'

const FOCUSABLE = 'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'

// Focus trap, Escape-to-close, scroll lock, and focus return for a dialog root.
export function useModal(rootRef, onClose) {
  let prevFocus = null

  function focusables() {
    return rootRef.value ? Array.from(rootRef.value.querySelectorAll(FOCUSABLE)) : []
  }

  function onKeydown(e) {
    if (e.key === 'Escape') { e.preventDefault(); onClose(); return }
    if (e.key !== 'Tab') return
    const els = focusables()
    if (!els.length) return
    const first = els[0], last = els[els.length - 1]
    if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus() }
    else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus() }
  }

  onMounted(() => {
    prevFocus = document.activeElement
    document.addEventListener('keydown', onKeydown)
    document.body.style.overflow = 'hidden'
    nextTick(() => { const els = focusables(); (els[0] || rootRef.value)?.focus() })
  })
  onBeforeUnmount(() => {
    document.removeEventListener('keydown', onKeydown)
    document.body.style.overflow = ''
    if (prevFocus && typeof prevFocus.focus === 'function') prevFocus.focus()
  })
}
