<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import { PopoverRoot, PopoverTrigger, PopoverPortal, PopoverContent } from 'reka-ui'

interface SelectItem {
  label: string
  value: string
}

const props = withDefaults(
  defineProps<{
    modelValue: string[]
    items: SelectItem[]
    placeholder?: string
    searchPlaceholder?: string
    searchable?: boolean
    disabled?: boolean
    loading?: boolean
    id?: string
    ariaLabel?: string
    contentWidth?: boolean
  }>(),
  {
    placeholder: 'Select...',
    searchPlaceholder: 'Search...',
    searchable: true,
    disabled: false,
    loading: false,
    id: undefined,
    ariaLabel: undefined,
    contentWidth: false,
  }
)

const emit = defineEmits<{
  (e: 'update:modelValue', value: string[]): void
}>()

const isOpen = ref(false)
const query = ref('')
const activeIndex = ref(-1)
const searchEl = ref<HTMLInputElement | null>(null)

const selectedLabels = computed(() =>
  props.items.filter(item => props.modelValue.includes(item.value)).map(item => item.label)
)

const triggerText = computed(() => {
  if (props.loading) return 'Loading...'
  if (!selectedLabels.value.length) return props.placeholder
  if (selectedLabels.value.length <= 2) return selectedLabels.value.join(', ')
  return `${selectedLabels.value.length} selected`
})

const filteredItems = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return props.items
  return props.items.filter(item => item.label.toLowerCase().includes(q))
})

function onOpenChange(open: boolean) {
  if (!open) {
    isOpen.value = false
    activeIndex.value = -1
    return
  }
  if (props.disabled || props.loading) return
  isOpen.value = true
  query.value = ''
  activeIndex.value = -1
  if (props.searchable) {
    nextTick(() => searchEl.value?.focus())
  }
}

function close() {
  onOpenChange(false)
}

function isSelected(value: string) {
  return props.modelValue.includes(value)
}

function toggleItem(item: SelectItem) {
  const next = isSelected(item.value)
    ? props.modelValue.filter(v => v !== item.value)
    : [...props.modelValue, item.value]
  emit('update:modelValue', next)
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
    if (item) toggleItem(item)
  }
}

watch(query, () => { activeIndex.value = 0 })
</script>

<template>
  <PopoverRoot :open="isOpen" @update:open="onOpenChange">
    <PopoverTrigger as-child>
      <button
        :id="id"
        type="button"
        class="flex items-center justify-between gap-1.5 px-2.5 border border-gray-200 dark:border-carbon-800 rounded-lg bg-white dark:bg-carbon-950/70 focus-within:border-yellow-400/60 focus:outline-hidden focus:border-yellow-400/60 focus:ring-1 focus:ring-yellow-400/40 transition-all duration-200 h-[38px] text-sm disabled:opacity-50 disabled:cursor-not-allowed"
        :class="contentWidth ? 'w-fit' : 'w-full'"
        :disabled="disabled || loading"
        :aria-label="ariaLabel"
        :aria-expanded="isOpen"
      >
        <span
          class="truncate text-left"
          :class="selectedLabels.length ? 'text-gray-900/90 dark:text-white/90' : 'text-gray-400 dark:text-wire-200/30'"
        >
          {{ triggerText }}
        </span>
        <UIcon :name="loading ? 'i-lucide-loader-2' : 'i-lucide-chevron-down'" class="w-3.5 h-3.5 text-gray-400 shrink-0 transition-transform" :class="loading ? 'animate-spin' : (isOpen ? 'rotate-180' : '')" />
      </button>
    </PopoverTrigger>

    <PopoverPortal>
      <PopoverContent
        :side-offset="4"
        align="start"
        class="z-50 w-max border border-gray-200 dark:border-carbon-800 rounded-lg bg-white dark:bg-carbon-950 shadow-lg overflow-hidden"
        :class="contentWidth ? '' : 'min-w-[var(--reka-popper-anchor-width)]'"
        @open-auto-focus="searchable ? $event.preventDefault() : undefined"
        @keydown="!searchable && onKeydown($event)"
      >
        <div v-if="searchable" class="p-1.5 border-b border-gray-200 dark:border-carbon-800">
          <input
            ref="searchEl"
            v-model="query"
            type="text"
            class="w-full bg-transparent border-0 p-1 focus:ring-0 focus:outline-hidden text-sm text-gray-900/90 dark:text-white/90 placeholder-gray-400 dark:placeholder-wire-200/30"
            :placeholder="searchPlaceholder"
            :aria-label="searchPlaceholder"
            @keydown="onKeydown"
          >
        </div>
        <ul class="max-h-56 overflow-y-auto py-1">
          <li
            v-for="(item, idx) in filteredItems"
            :key="item.value"
            class="px-2.5 py-1.5 text-sm cursor-pointer flex items-center justify-between gap-2"
            :class="idx === activeIndex ? 'bg-yellow-400/10 text-yellow-600 dark:text-yellow-400' : 'text-gray-700 dark:text-wire-200 hover:bg-gray-100 dark:hover:bg-carbon-900/60'"
            @mouseenter="activeIndex = idx"
            @click="toggleItem(item)"
          >
            <span class="flex items-center gap-2 min-w-0 truncate">
              <span
                class="w-4 h-4 rounded border shrink-0 flex items-center justify-center"
                :class="isSelected(item.value) ? 'bg-yellow-400 border-yellow-400' : 'border-gray-300 dark:border-carbon-700'"
              >
                <UIcon v-if="isSelected(item.value)" name="i-lucide-check" class="w-3 h-3 text-carbon-950" />
              </span>
              <span class="truncate">{{ item.label }}</span>
            </span>
          </li>
          <li v-if="filteredItems.length === 0" class="px-2.5 py-2 text-xs text-gray-400 dark:text-wire-200/30 italic text-center">
            No results found.
          </li>
        </ul>
      </PopoverContent>
    </PopoverPortal>
  </PopoverRoot>
</template>
