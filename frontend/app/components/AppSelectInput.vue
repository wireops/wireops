<script setup lang="ts">
import { ref, computed, watch, nextTick, onMounted, onUnmounted } from 'vue'

interface SelectItem {
  label: string
  value: string
}

const props = withDefaults(
  defineProps<{
    modelValue: string
    items: SelectItem[]
    placeholder?: string
    searchPlaceholder?: string
    searchable?: boolean
    disabled?: boolean
    id?: string
    ariaLabel?: string
  }>(),
  {
    placeholder: 'Select...',
    searchPlaceholder: 'Search...',
    searchable: true,
    disabled: false,
    id: undefined,
    ariaLabel: undefined,
  }
)

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
}>()

const isOpen = ref(false)
const query = ref('')
const activeIndex = ref(-1)
const rootEl = ref<HTMLElement | null>(null)
const searchEl = ref<HTMLInputElement | null>(null)
const triggerEl = ref<HTMLButtonElement | null>(null)

const selectedItem = computed(() =>
  props.items.find(item => item.value === props.modelValue)
)

const filteredItems = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return props.items
  return props.items.filter(item => item.label.toLowerCase().includes(q))
})

function open() {
  if (isOpen.value || props.disabled) return
  isOpen.value = true
  query.value = ''
  activeIndex.value = props.items.findIndex(item => item.value === props.modelValue)
  if (props.searchable) {
    nextTick(() => searchEl.value?.focus())
  }
}

function close() {
  const searchWasFocused = props.searchable && document.activeElement === searchEl.value
  isOpen.value = false
  activeIndex.value = -1
  if (searchWasFocused) {
    nextTick(() => triggerEl.value?.focus())
  }
}

function toggle() {
  if (isOpen.value) close()
  else open()
}

function selectItem(item: SelectItem) {
  emit('update:modelValue', item.value)
  close()
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    close()
    return
  }
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    activeIndex.value = Math.min(activeIndex.value + 1, filteredItems.value.length - 1)
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    activeIndex.value = Math.max(activeIndex.value - 1, 0)
  } else if (e.key === 'Enter') {
    e.preventDefault()
    const item = filteredItems.value[activeIndex.value]
    if (item) selectItem(item)
  }
}

watch(query, () => { activeIndex.value = 0 })

function onClickOutside(e: MouseEvent) {
  if (rootEl.value && !rootEl.value.contains(e.target as Node)) {
    close()
  }
}

onMounted(() => document.addEventListener('mousedown', onClickOutside))
onUnmounted(() => document.removeEventListener('mousedown', onClickOutside))
</script>

<template>
  <div ref="rootEl" class="relative">
    <button
      :id="id"
      ref="triggerEl"
      type="button"
      class="flex items-center justify-between gap-1.5 px-2.5 border border-gray-200 dark:border-carbon-800 rounded-lg bg-white dark:bg-carbon-950/70 focus-within:border-yellow-400/60 focus:outline-hidden focus:border-yellow-400/60 focus:ring-1 focus:ring-yellow-400/40 transition-all duration-200 w-full h-[38px] text-sm disabled:opacity-50 disabled:cursor-not-allowed"
      :disabled="disabled"
      :aria-label="ariaLabel"
      :aria-expanded="isOpen"
      @click="toggle"
      @keydown="!searchable && onKeydown($event)"
    >
      <span
        class="truncate text-left"
        :class="selectedItem ? 'text-gray-900/90 dark:text-white/90' : 'text-gray-400 dark:text-wire-200/30'"
      >
        {{ selectedItem ? selectedItem.label : placeholder }}
      </span>
      <UIcon name="i-lucide-chevron-down" class="w-3.5 h-3.5 text-gray-400 shrink-0 transition-transform" :class="isOpen ? 'rotate-180' : ''" />
    </button>

    <div
      v-if="isOpen"
      class="absolute z-50 mt-1 w-max min-w-full border border-gray-200 dark:border-carbon-800 rounded-lg bg-white dark:bg-carbon-950 shadow-lg overflow-hidden"
    >
      <div v-if="searchable" class="p-1.5 border-b border-gray-200 dark:border-carbon-800">
        <input
          ref="searchEl"
          v-model="query"
          type="text"
          class="w-full bg-transparent border-0 p-1 focus:ring-0 focus:outline-hidden text-sm text-gray-900/90 dark:text-white/90 placeholder-gray-400 dark:placeholder-wire-200/30"
          :placeholder="searchPlaceholder"
          @keydown="onKeydown"
        >
      </div>
      <ul class="max-h-48 overflow-y-auto py-1">
        <li
          v-for="(item, idx) in filteredItems"
          :key="item.value"
          class="px-2.5 py-1.5 text-sm cursor-pointer flex items-center justify-between"
          :class="idx === activeIndex ? 'bg-yellow-400/10 text-yellow-600 dark:text-yellow-400' : 'text-gray-700 dark:text-wire-200 hover:bg-gray-100 dark:hover:bg-carbon-900/60'"
          @mouseenter="activeIndex = idx"
          @click="selectItem(item)"
        >
          <span class="truncate">{{ item.label }}</span>
          <UIcon v-if="item.value === modelValue" name="i-lucide-check" class="w-3.5 h-3.5 text-yellow-500 shrink-0" />
        </li>
        <li v-if="filteredItems.length === 0" class="px-2.5 py-2 text-xs text-gray-400 dark:text-wire-200/30 italic text-center">
          No results found.
        </li>
      </ul>
    </div>
  </div>
</template>
