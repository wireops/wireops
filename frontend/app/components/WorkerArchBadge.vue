<script setup lang="ts">
import { archIcon } from '../utils/worker'

withDefaults(defineProps<{
  arch?: string
  size?: 'xs' | 'sm' | 'md' | 'lg' | 'xl'
}>(), {
  arch: '',
  size: 'xs',
})
</script>

<template>
  <UBadge
    v-if="arch"
    :icon="archIcon(arch)"
    :label="arch"
    variant="subtle"
    color="neutral"
    :size="size"
    class="font-mono"
  />
</template>
