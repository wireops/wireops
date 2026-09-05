<script setup lang="ts">
import { computed } from 'vue'
import { isWorkerVersionOutdated } from '../utils/worker'

const props = withDefaults(defineProps<{
  version?: string
  size?: 'xs' | 'sm' | 'md' | 'lg' | 'xl'
}>(), {
  version: '',
  size: 'xs',
})

const { serverVersion } = useServerVersion()
const isOutdated = computed(() => isWorkerVersionOutdated(props.version, serverVersion.value))
</script>

<template>
  <UTooltip v-if="version && isOutdated" :text="`Worker is behind server version ${serverVersion}`">
    <UBadge
      :label="version.startsWith('v') ? version : `v${version}`"
      icon="i-lucide-alert-triangle"
      variant="subtle"
      color="warning"
      :size="size"
      class="font-mono"
    />
  </UTooltip>
  <UBadge
    v-else-if="version"
    :label="version.startsWith('v') ? version : `v${version}`"
    variant="subtle"
    color="neutral"
    :size="size"
    class="font-mono"
  />
  <UTooltip v-else text="Worker did not report its version">
    <UBadge label="outdated agent" variant="subtle" color="warning" :size="size" />
  </UTooltip>
</template>
