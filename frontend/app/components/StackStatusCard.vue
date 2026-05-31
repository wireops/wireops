<script setup lang="ts">
import { computed } from 'vue'
import type { StackStatusDisplay } from '../utils/stack-status'

const props = defineProps<{
  title: string
  status: StackStatusDisplay
  tooltip?: string
}>()

const cardClass = computed(() => {
  switch (props.status.color) {
    case 'success':
      return 'border-emerald-200 bg-emerald-50 text-emerald-950 dark:border-emerald-500/30 dark:bg-emerald-500/12 dark:text-emerald-100'
    case 'primary':
    case 'info':
      return 'border-cyan-200 bg-cyan-50 text-cyan-950 dark:border-cyan-500/30 dark:bg-cyan-500/12 dark:text-cyan-100'
    case 'warning':
      return 'border-amber-200 bg-amber-50 text-amber-950 dark:border-amber-500/30 dark:bg-amber-500/12 dark:text-amber-100'
    case 'error':
      return 'border-red-200 bg-red-50 text-red-950 dark:border-red-500/30 dark:bg-red-500/12 dark:text-red-100'
    default:
      return 'border-gray-200 bg-gray-50 text-gray-950 dark:border-carbon-700 dark:bg-carbon-900/55 dark:text-wire-200'
  }
})

const iconClass = computed(() => {
  switch (props.status.color) {
    case 'success':
      return 'bg-emerald-500/15 text-emerald-600 dark:bg-emerald-400/15 dark:text-emerald-300'
    case 'primary':
    case 'info':
      return 'bg-cyan-500/15 text-cyan-600 dark:bg-cyan-400/15 dark:text-cyan-300'
    case 'warning':
      return 'bg-amber-500/15 text-amber-600 dark:bg-amber-400/15 dark:text-amber-300'
    case 'error':
      return 'bg-red-500/15 text-red-600 dark:bg-red-400/15 dark:text-red-300'
    default:
      return 'bg-gray-500/10 text-gray-500 dark:bg-white/5 dark:text-wire-200/60'
  }
})
</script>

<template>
  <div
    class="flex h-full min-h-24 w-full min-w-0 flex-col justify-between gap-3 rounded-md border p-3 sm:min-h-20 sm:flex-row sm:items-center sm:p-4"
    :class="cardClass"
    :title="tooltip"
  >
    <div class="min-w-0">
      <p class="truncate text-[10px] font-semibold uppercase tracking-wide opacity-70 sm:text-xs">
        {{ title }}
      </p>
      <p class="mt-1 truncate text-xs font-light uppercase sm:text-sm">
        {{ status.label }}
      </p>
    </div>
    <div class="flex h-9 w-9 shrink-0 items-center justify-center rounded-md sm:h-10 sm:w-10" :class="iconClass">
      <UIcon :name="status.icon" class="h-4 w-4 sm:h-5 sm:w-5" />
    </div>
  </div>
</template>
