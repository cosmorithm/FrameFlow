<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, nextTick } from 'vue'
import Icon from './Icon.vue'
defineProps<{ title: string; drawer?: boolean }>()
const emit = defineEmits<{ (e: 'close'): void }>()
const box = ref<HTMLElement | null>(null)
let previous: HTMLElement | null = null
function key(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.preventDefault()
    emit('close')
  }
  if (e.key !== 'Tab' || !box.value) return
  const nodes = Array.from(
    box.value.querySelectorAll<HTMLElement>(
      'button:enabled, input:enabled:not([type=hidden]), textarea:enabled, a[href]',
    ),
  ).filter((n) => n.getClientRects().length > 0)
  const first = nodes[0],
    last = nodes[nodes.length - 1]
  if (e.shiftKey && (document.activeElement === first || document.activeElement === box.value)) {
    e.preventDefault()
    last?.focus()
  } else if (!e.shiftKey && document.activeElement === last) {
    e.preventDefault()
    first?.focus()
  }
}
onMounted(() => {
  previous = document.activeElement as HTMLElement
  void nextTick(() => box.value?.focus())
  document.addEventListener('keydown', key)
})
onBeforeUnmount(() => {
  document.removeEventListener('keydown', key)
  previous?.focus()
})
</script>
<template>
  <div class="overlay" :class="{ drawer }" @mousedown.self="emit('close')">
    <section ref="box" class="dialog" role="dialog" aria-modal="true" :aria-label="title" tabindex="-1">
      <header class="dialog-head">
        <h2>{{ title }}</h2>
        <button type="button" class="icon-button" aria-label="关闭" @click="emit('close')">
          <Icon name="x" />
        </button>
      </header>
      <slot />
    </section>
  </div>
</template>
