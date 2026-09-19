<template>
  <Teleport to="body">
    <Transition name="modal">
      <div
        v-if="show"
        ref="overlayRef"
        class="modal-overlay"
        :style="zIndexStyle"
        :aria-labelledby="dialogId"
        role="dialog"
        aria-modal="true"
        @click.self="handleClose"
      >
        <!-- Modal panel -->
        <div ref="dialogRef" :class="['modal-content', widthClasses]" :tabindex="trapFocus ? -1 : undefined" @click.stop>
          <!-- Header -->
          <div class="modal-header">
            <h3 :id="dialogId" class="modal-title">
              {{ title }}
            </h3>
            <button
              v-if="showCloseButton"
              @click="emit('close')"
              class="-mr-2 rounded-xl p-2 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/30 focus-visible:ring-offset-2 dark:text-dark-500 dark:hover:bg-dark-700 dark:hover:text-dark-300 dark:focus-visible:ring-offset-dark-900"
              aria-label="Close modal"
            >
              <Icon name="x" size="md" />
            </button>
          </div>

          <!-- Body -->
          <div ref="modalBodyRef" class="modal-body">
            <slot></slot>
          </div>

          <!-- Footer -->
          <div v-if="$slots.footer" class="modal-footer">
            <slot name="footer"></slot>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script lang="ts">
// Shared across instances so a lower dialog never competes with a nested one.
interface OpenDialog {
  overlay: HTMLElement
  panel: HTMLElement
  trapFocus: boolean
  zIndex: number
}
const openDialogs: OpenDialog[] = []
const inertBackground = new Map<HTMLElement, string | null>()
let dialogIdCounter = 0

function topDialog(): OpenDialog | undefined {
  return openDialogs.reduce<OpenDialog | undefined>((top, entry) =>
    !top || entry.zIndex >= top.zIndex ? entry : top, undefined)
}

function syncBackgroundInert() {
  for (const [element, value] of inertBackground) {
    if (value === null) element.removeAttribute('inert')
    else element.setAttribute('inert', value)
  }
  inertBackground.clear()
  const top = topDialog()
  if (!top || !openDialogs.some(entry => entry.trapFocus)) return
  for (const element of Array.from(document.body.children)) {
    if (!(element instanceof HTMLElement) || element === top.overlay || element.contains(top.overlay)) continue
    inertBackground.set(element, element.getAttribute('inert'))
    element.setAttribute('inert', '')
  }
}

function focusableControls(panel: HTMLElement): HTMLElement[] {
  return Array.from(panel.querySelectorAll<HTMLElement>(
    'button, [href], input, select, textarea, [tabindex], [contenteditable="true"]'
  )).filter(element => {
    if (element.tabIndex < 0 || element.matches(':disabled, input[type="hidden"]') || element.closest('[hidden], [inert]')) return false
    for (let parent: HTMLElement | null = element; parent; parent = parent.parentElement) {
      const style = getComputedStyle(parent)
      if (style.display === 'none' || style.visibility === 'hidden' || style.visibility === 'collapse') return false
      if (parent === panel) break
    }
    return true
  }).sort((a, b) => (a.tabIndex || Infinity) - (b.tabIndex || Infinity))
}
</script>

<script setup lang="ts">
import { computed, watch, onMounted, onUnmounted, ref, nextTick } from 'vue'
import Icon from '@/components/icons/Icon.vue'

// 生成唯一ID以避免多个对话框时ID冲突
const dialogId = `modal-title-${++dialogIdCounter}`

// 焦点管理
const dialogRef = ref<HTMLElement | null>(null)
const overlayRef = ref<HTMLElement | null>(null)
const modalBodyRef = ref<HTMLElement | null>(null)
let previousActiveElement: HTMLElement | null = null
let openEntry: OpenDialog | undefined
let disposed = false
let openGeneration = 0

type DialogWidth = 'narrow' | 'normal' | 'wide' | 'extra-wide' | 'full'

interface Props {
  show: boolean
  title: string
  width?: DialogWidth
  closeOnEscape?: boolean
  closeOnClickOutside?: boolean
  showCloseButton?: boolean
  trapFocus?: boolean
  zIndex?: number
}

interface Emits {
  (e: 'close'): void
}

const props = withDefaults(defineProps<Props>(), {
  width: 'normal',
  closeOnEscape: true,
  closeOnClickOutside: false,
  showCloseButton: true,
  trapFocus: false,
  zIndex: 50
})

const emit = defineEmits<Emits>()

// Custom z-index style (overrides the default z-50 from CSS)
const zIndexStyle = computed(() => {
  return props.zIndex !== 50 ? { zIndex: props.zIndex } : undefined
})

const widthClasses = computed(() => {
  // Width guidance: narrow=confirm/short prompts, normal=standard forms,
  // wide=multi-section forms or rich content, extra-wide=analytics/tables,
  // full=full-screen or very dense layouts.
  const widths: Record<DialogWidth, string> = {
    narrow: 'max-w-md',
    normal: 'max-w-lg',
    wide: 'w-full sm:max-w-2xl md:max-w-3xl lg:max-w-4xl',
    'extra-wide': 'w-full sm:max-w-3xl md:max-w-4xl lg:max-w-5xl xl:max-w-6xl',
    full: 'w-full sm:max-w-4xl md:max-w-5xl lg:max-w-6xl xl:max-w-7xl'
  }
  return widths[props.width]
})

const handleClose = () => {
  if (props.closeOnClickOutside) {
    emit('close')
  }
}

const handleEscape = (event: KeyboardEvent) => {
  if (openDialogs.some(entry => entry.trapFocus) && topDialog() !== openEntry) return
  if (props.show && props.closeOnEscape && event.key === 'Escape') {
    emit('close')
  }
}

function activeTrap() {
  return props.show && props.trapFocus && openEntry !== undefined && topDialog() === openEntry
}

function focusInside() {
  if (dialogRef.value) (focusableControls(dialogRef.value)[0] || dialogRef.value).focus()
}

function handleTab(event: KeyboardEvent) {
  if (!activeTrap() || event.key !== 'Tab' || event.defaultPrevented || !dialogRef.value) return
  const controls = focusableControls(dialogRef.value)
  const first = controls[0]
  const last = controls[controls.length - 1]
  const focused = document.activeElement
  if (!first || !dialogRef.value.contains(focused) || focused === dialogRef.value) {
    event.preventDefault()
    ;(event.shiftKey ? last : first)?.focus()
    if (!first) dialogRef.value.focus()
  } else if (event.shiftKey && focused === first) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && focused === last) {
    event.preventDefault()
    first.focus()
  }
}

function handleFocus(event: FocusEvent) {
  if (activeTrap() && event.target instanceof Node && !dialogRef.value?.contains(event.target)) focusInside()
}

function releaseDialog() {
  const wasTop = openEntry !== undefined && topDialog() === openEntry
  if (openEntry) {
    openDialogs.splice(openDialogs.indexOf(openEntry), 1)
    openEntry = undefined
    syncBackgroundInert()
  }
  if (!openDialogs.length) document.body.classList.remove('modal-open')
  const top = topDialog()
  if (wasTop && previousActiveElement?.isConnected && (!top || top.panel.contains(previousActiveElement))) {
    previousActiveElement.focus()
  }
  previousActiveElement = null
}

// Prevent body scroll when modal is open and manage focus
watch(
  () => props.show,
  async (isOpen) => {
    const generation = ++openGeneration
    if (isOpen) {
      // 保存当前焦点元素
      previousActiveElement = document.activeElement as HTMLElement
      // 使用CSS类而不是直接操作style,更易于管理多个对话框
      document.body.classList.add('modal-open')

      // 等待DOM更新后设置焦点到对话框
      await nextTick()
      if (disposed || !props.show || generation !== openGeneration || !overlayRef.value || !dialogRef.value) return
      openEntry = { overlay: overlayRef.value, panel: dialogRef.value, trapFocus: props.trapFocus, zIndex: props.zIndex }
      openDialogs.push(openEntry)
      syncBackgroundInert()
      if (modalBodyRef.value) {
        modalBodyRef.value.scrollTop = 0
      }
      if (topDialog() === openEntry && props.trapFocus) {
        focusInside()
      } else if (topDialog() === openEntry) {
        const firstFocusable = dialogRef.value.querySelector<HTMLElement>(
          'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
        )
        firstFocusable?.focus()
      }
    } else {
      releaseDialog()
    }
  },
  { immediate: true }
)

watch(() => [props.trapFocus, props.zIndex], () => {
  if (!openEntry) return
  openEntry.trapFocus = props.trapFocus
  openEntry.zIndex = props.zIndex
  syncBackgroundInert()
  if (activeTrap() && !dialogRef.value?.contains(document.activeElement)) focusInside()
})

onMounted(() => {
  document.addEventListener('keydown', handleEscape)
  document.addEventListener('keydown', handleTab)
  document.addEventListener('focusin', handleFocus)
})

onUnmounted(() => {
  disposed = true
  document.removeEventListener('keydown', handleEscape)
  document.removeEventListener('keydown', handleTab)
  document.removeEventListener('focusin', handleFocus)
  // 确保组件卸载时移除滚动锁定
  releaseDialog()
})
</script>
