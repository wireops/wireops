<script setup lang="ts">
withDefaults(defineProps<{
  dockerVersion?: string
  dockerOnline?: boolean
  size?: 'xs' | 'sm' | 'md' | 'lg' | 'xl'
}>(), {
  dockerVersion: '',
  size: 'xs',
})
</script>

<template>
  <UBadge
    v-if="dockerVersion"
    icon="i-lucide-container"
    :label="dockerOnline === false ? 'docker offline' : `docker ${dockerVersion}`"
    variant="subtle"
    :color="dockerOnline === false ? 'error' : 'neutral'"
    :size="size"
    class="font-mono"
  />
</template>
