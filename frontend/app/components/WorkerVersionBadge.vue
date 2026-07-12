<script setup lang="ts">
withDefaults(defineProps<{
  version?: string
  size?: 'xs' | 'sm' | 'md' | 'lg' | 'xl'
}>(), {
  version: '',
  size: 'xs',
})
</script>

<template>
  <UBadge
    v-if="version"
    :label="`v${version}`"
    variant="subtle"
    color="neutral"
    :size="size"
    class="font-mono"
  />
  <UTooltip v-else text="Worker did not report its version">
    <UBadge label="outdated agent" variant="subtle" color="warning" :size="size" />
  </UTooltip>
</template>
