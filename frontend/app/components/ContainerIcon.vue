<script lang="ts">
import { reactive } from 'vue'

// Shared/global reactive set to track failed slugs across all instances.
const failedSlugs = reactive(new Set<string>())
</script>

<script setup lang="ts">
const props = defineProps<{
  name?: string
  slug?: string
  iconClass?: string
  wrapperClass?: string
}>()

const getIconUrl = (slug: string) => `https://cdn.jsdelivr.net/gh/selfhst/icons/svg/${encodeURIComponent(slug)}.svg`

const handleIconError = (slug: string) => {
  failedSlugs.add(slug)
}
</script>

<template>
  <div :class="wrapperClass || 'w-7 h-7 flex flex-shrink-0 items-center justify-center rounded-md bg-gray-100 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 overflow-hidden'">
    <template v-if="slug && !failedSlugs.has(slug)">
      <!-- CDN image -->
      <img
        :src="getIconUrl(slug)"
        :class="iconClass || 'w-4 h-4 object-contain'"
        :alt="name"
        loading="lazy"
        @error="handleIconError(slug)"
      >
    </template>
    <template v-else>
      <!-- Fallback lucide icon -->
      <UIcon
        name="i-lucide-box"
        :class="iconClass || 'w-4 h-4 text-gray-500 dark:text-gray-400'"
      />
    </template>
  </div>
</template>
