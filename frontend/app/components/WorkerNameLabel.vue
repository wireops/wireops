<script setup lang="ts">
defineProps<{
  name: string
}>()
</script>

<template>
  <code class="inline-flex items-center font-mono text-xs px-1.5 py-0.5 rounded bg-gray-100 dark:bg-carbon-800 text-gray-900 dark:text-wire-200 border border-gray-200 dark:border-carbon-700/60 select-all">
    {{ name }}
  </code>
</template>
