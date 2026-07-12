<script setup lang="ts">
withDefaults(defineProps<{
  dockerVersion?: string
  dockerOnline?: boolean
  size?: 'xs' | 'sm' | 'md' | 'lg' | 'xl'
}>(), {
  dockerVersion: '',
  dockerOnline: false,
  size: 'xs',
})
</script>

<template>
  <UBadge
    v-if="dockerVersion"
    icon="i-lucide-container"
    :label="dockerOnline ? `docker ${dockerVersion}` : 'docker offline'"
    variant="subtle"
    :color="dockerOnline ? 'neutral' : 'error'"
    :size="size"
    class="font-mono"
  />
</template>
