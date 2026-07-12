<script setup lang="ts">
withDefaults(defineProps<{
  composeVersion?: string
  size?: 'xs' | 'sm' | 'md' | 'lg' | 'xl'
}>(), {
  composeVersion: '',
  size: 'xs',
})
</script>

<template>
  <UBadge
    v-if="composeVersion"
    icon="i-lucide-layers-2"
    :label="`compose ${composeVersion}`"
    variant="subtle"
    color="neutral"
    :size="size"
    class="font-mono"
  />
  <UBadge v-else label="no compose" variant="subtle" color="warning" :size="size" />
</template>
