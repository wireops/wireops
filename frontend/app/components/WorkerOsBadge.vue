<script setup lang="ts">
import { osIcon, osLabel } from '../utils/worker'

withDefaults(defineProps<{
  os?: string
  size?: 'xs' | 'sm' | 'md' | 'lg' | 'xl'
}>(), {
  os: '',
  size: 'xs',
})
</script>

<template>
  <UBadge
    :icon="osIcon(os)"
    :label="osLabel(os)"
    variant="subtle"
    color="neutral"
    :size="size"
    class="font-mono"
  />
</template>
