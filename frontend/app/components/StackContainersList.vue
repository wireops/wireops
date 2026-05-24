<script setup lang="ts">
import { computed } from 'vue'
import ContainerIcon from './ContainerIcon.vue'

export interface ContainerInfo {
  name: string
  is_fallback: boolean
  slug?: string
}

const props = defineProps<{
  containers: ContainerInfo[]
}>()

const maxDisplay = 3

const visibleContainers = computed(() => {
  if (!props.containers) return []
  return props.containers.slice(0, maxDisplay)
})

const remainingCount = computed(() => {
  if (!props.containers) return 0
  return Math.max(0, props.containers.length - maxDisplay)
})
</script>

<template>
  <div class="flex items-center gap-1.5 flex-wrap">
    <UTooltip
      v-for="container in visibleContainers"
      :key="container.name"
      :text="container.name"
    >
      <ContainerIcon
        :name="container.name"
        :slug="container.slug"
      />
    </UTooltip>

    <div
      v-if="remainingCount > 0"
      class="w-7 h-7 flex flex-shrink-0 items-center justify-center rounded-md bg-gray-100 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 text-xs font-medium text-gray-500 dark:text-gray-400"
      :title="`${remainingCount} more container${remainingCount > 1 ? 's' : ''}`"
    >
      +{{ remainingCount }}
    </div>
  </div>
</template>
