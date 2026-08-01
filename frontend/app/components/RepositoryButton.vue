<script setup lang="ts">
import { computed } from 'vue'

// Repository reference used on stack/job detail pages — a link to the
// repository's own page with its provider icon (via RepositoryIcon) and
// name, so every "which repo is this from" spot in the app looks the same.
// Text color follows the provider instead of the default link color:
// GitHub reads as a grayish-white (matching GithubIcon's own tone), any
// other/unrecognized platform (generic git URL) reads as plain gray.
const props = withDefaults(defineProps<{
  repository?: { id: string, name: string, platform?: string | null } | null
  iconClass?: string
}>(), {
  repository: null,
  iconClass: 'w-3.5 h-3.5 shrink-0',
})

const textClass = computed(() =>
  props.repository?.platform === 'github'
    ? 'text-gray-600 dark:text-gray-300'
    : 'text-gray-500 dark:text-gray-400'
)
</script>

<template>
  <NuxtLink
    v-if="repository"
    :to="`/repositories/${repository.id}`"
    target="_blank"
    rel="noopener noreferrer"
    :aria-label="`${repository.name} (opens repository in a new tab)`"
    class="inline-flex items-center gap-1.5 rounded-md border border-gray-200 dark:border-gray-700 px-2 py-1 hover:border-primary-500 hover:bg-gray-50 dark:hover:bg-gray-900 transition-colors"
    :class="textClass"
  >
    <RepositoryIcon :repository="repository" :icon-class="iconClass" />
    {{ repository.name }}
  </NuxtLink>
</template>
