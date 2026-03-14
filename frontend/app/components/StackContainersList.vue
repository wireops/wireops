<script setup lang="ts">
import { computed } from 'vue'

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

const getIconUrl = (slug: string) => `https://cdn.jsdelivr.net/gh/selfhst/icons/svg/${slug}.svg`
</script>

<template>
  <div class="flex items-center gap-1.5 flex-wrap">
    <UTooltip
      v-for="container in visibleContainers"
      :key="container.name"
      :text="container.name"
    >
      <div class="w-7 h-7 flex flex-shrink-0 items-center justify-center rounded-md bg-gray-100 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 overflow-hidden">
        <template v-if="container.slug">
          <!-- CDN image -->
          <img
            :src="getIconUrl(container.slug)"
            class="w-4 h-4 object-contain"
            :alt="container.name"
            loading="lazy"
          />
        </template>
        <template v-else>
          <!-- Fallback lucide icon -->
          <UIcon
            name="i-lucide-box"
            class="w-4 h-4 text-gray-500 dark:text-gray-400"
          />
        </template>
      </div>
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
