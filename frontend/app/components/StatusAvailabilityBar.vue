<script setup lang="ts">
import { computed } from 'vue'

export interface AvailabilitySegment {
  key: string
  label: string
  barClass: string
  dotClass: string
  statuses: string[]
  filterValue?: string
}

const props = defineProps<{
  items: any[]
  segments: AvailabilitySegment[]
  modelValue: string
  ariaLabel: string
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
}>()

const breakdown = computed(() => {
  const total = props.items.length
  const known = new Set(props.segments.flatMap((s) => s.statuses))
  const rows = props.segments.map((segment) => {
    const count = props.items.filter((item: any) => segment.statuses.includes(item.status)).length
    return { ...segment, count, percent: total > 0 ? (count / total) * 100 : 0 }
  })
  const otherCount = props.items.filter((item: any) => !known.has(item.status)).length
  if (otherCount > 0) {
    rows.push({
      key: 'other',
      label: 'Other',
      barClass: 'bg-gray-300 dark:bg-carbon-600',
      dotClass: 'bg-gray-300 dark:bg-carbon-600',
      statuses: [],
      count: otherCount,
      percent: total > 0 ? (otherCount / total) * 100 : 0,
    })
  }
  return rows.filter((r) => r.count > 0)
})

function toggleFilter(segment: AvailabilitySegment) {
  const targetStatus = segment.filterValue ?? segment.key
  emit('update:modelValue', props.modelValue === targetStatus ? 'all' : targetStatus)
}

function isActive(segment: AvailabilitySegment) {
  const targetStatus = segment.filterValue ?? segment.key
  return props.modelValue === targetStatus || segment.statuses.includes(props.modelValue)
}
</script>

<template>
  <div v-if="items.length" class="space-y-2">
    <div class="flex w-full gap-0.5 h-2.5" role="img" :aria-label="ariaLabel">
      <UTooltip
        v-for="segment in breakdown"
        :key="segment.key"
        :text="`${segment.label}: ${segment.count} (${Math.round(segment.percent)}%)`"
      >
        <button
          type="button"
          class="h-full transition-opacity hover:opacity-80 first:rounded-l-full last:rounded-r-full"
          :class="[segment.barClass, isActive(segment) ? 'ring-2 ring-offset-1 ring-offset-white dark:ring-offset-carbon-950 ring-gray-900/40 dark:ring-white/40' : '']"
          :style="{ width: `${segment.percent}%` }"
          :aria-label="`${segment.label}: ${segment.count} items`"
          :disabled="segment.key === 'other'"
          @click="segment.key !== 'other' && toggleFilter(segment)"
        />
      </UTooltip>
    </div>
    <div class="flex flex-wrap items-center gap-x-4 gap-y-1">
      <button
        v-for="segment in breakdown"
        :key="segment.key"
        type="button"
        class="flex items-center gap-1.5 text-xs text-gray-500 dark:text-wire-200/50 hover:text-gray-900 dark:hover:text-wire-200 transition-colors"
        :class="isActive(segment) ? 'font-semibold text-gray-900 dark:text-wire-200' : ''"
        :disabled="segment.key === 'other'"
        @click="segment.key !== 'other' && toggleFilter(segment)"
      >
        <span class="w-2 h-2 rounded-full shrink-0" :class="segment.dotClass" />
        {{ segment.label }} ({{ segment.count }})
      </button>
    </div>
  </div>
</template>
