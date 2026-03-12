<script setup lang="ts">
defineProps<{
  category: string
}>()
</script>

<template>
  <div class="flex items-center gap-2 mb-4">
    <UBadge variant="subtle" color="primary" size="sm" class="uppercase tracking-wider font-semibold">
      {{ category }}
    </UBadge>
    <div class="h-px flex-1 bg-gray-200 dark:bg-gray-800"></div>
  </div>
</template>
