<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import { PopoverRoot, PopoverTrigger, PopoverPortal, PopoverContent } from 'reka-ui'

interface SelectItem {
  label: string
  value: string
  icon?: string
  avatar?: { src: string }
}

const props = withDefaults(
  defineProps<{
    modelValue: string
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
  (e: 'update:modelValue', value: string): void
}>()

const isOpen = ref(false)
const query = ref('')
const activeIndex = ref(-1)
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

function onOpenChange(open: boolean) {
  if (!open) {
    isOpen.value = false
    activeIndex.value = -1
    return
  }
  if (props.disabled || props.loading) return
  isOpen.value = true
  query.value = ''
  activeIndex.value = props.items.findIndex(item => item.value === props.modelValue)
  if (props.searchable) {
    nextTick(() => searchEl.value?.focus())
  }
}

function close() {
  onOpenChange(false)
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
</script>

<template>
  <PopoverRoot :open="isOpen" @update:open="onOpenChange">
    <PopoverTrigger as-child>
      <button
        :id="id"
        ref="triggerEl"
        type="button"
        class="flex items-center justify-between gap-1.5 px-2.5 border border-gray-200 dark:border-carbon-800 rounded-lg bg-white dark:bg-carbon-950/70 focus-within:border-yellow-400/60 focus:outline-hidden focus:border-yellow-400/60 focus:ring-1 focus:ring-yellow-400/40 transition-all duration-200 h-[38px] text-sm disabled:opacity-50 disabled:cursor-not-allowed"
        :class="contentWidth ? 'w-fit' : 'w-full'"
        :disabled="disabled || loading"
        :aria-label="ariaLabel"
        :aria-expanded="isOpen"
      >
        <span class="flex items-center gap-1.5 min-w-0">
          <UIcon v-if="selectedItem?.icon" :name="selectedItem.icon" class="w-4 h-4 shrink-0 text-gray-400 dark:text-wire-200/30" />
          <img v-else-if="selectedItem?.avatar" :src="selectedItem.avatar.src" class="w-4 h-4 shrink-0 object-contain">
          <span
            class="truncate text-left"
            :class="selectedItem ? 'text-gray-900/90 dark:text-white/90' : 'text-gray-400 dark:text-wire-200/30'"
          >
            {{ loading ? 'Loading...' : (selectedItem ? selectedItem.label : placeholder) }}
          </span>
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
        <ul class="max-h-48 overflow-y-auto py-1">
          <li
            v-for="(item, idx) in filteredItems"
            :key="item.value"
            class="px-2.5 py-1.5 text-sm cursor-pointer flex items-center justify-between"
            :class="idx === activeIndex ? 'bg-yellow-400/10 text-yellow-600 dark:text-yellow-400' : 'text-gray-700 dark:text-wire-200 hover:bg-gray-100 dark:hover:bg-carbon-900/60'"
            @mouseenter="activeIndex = idx"
            @click="selectItem(item)"
          >
            <span class="flex items-center gap-1.5 min-w-0 truncate">
              <UIcon v-if="item.icon" :name="item.icon" class="w-4 h-4 shrink-0 text-gray-400 dark:text-wire-200/30" />
              <img v-else-if="item.avatar" :src="item.avatar.src" class="w-4 h-4 shrink-0 object-contain">
              <span class="truncate">{{ item.label }}</span>
            </span>
            <UIcon v-if="item.value === modelValue" name="i-lucide-check" class="w-3.5 h-3.5 text-yellow-500 shrink-0" />
          </li>
          <li v-if="filteredItems.length === 0" class="px-2.5 py-2 text-xs text-gray-400 dark:text-wire-200/30 italic text-center">
            No results found.
          </li>
        </ul>
      </PopoverContent>
    </PopoverPortal>
  </PopoverRoot>
</template>
