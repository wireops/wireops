<script setup lang="ts">
import { computed } from 'vue'

defineOptions({ inheritAttrs: false })

const props = withDefaults(defineProps<{
  status: string
  mobileIconOnly?: boolean
}>(), {
  mobileIconOnly: false,
})

const statusColor = computed(() => {
  switch (props.status?.toLowerCase()) {
    case 'active':
    case 'success':
    case 'connected':
    case 'running':
      return 'success'
    case 'error':
    case 'exited':
      return 'error'
    case 'paused':
    case 'pending':
    case 'queued':
    case 'stalled':
    case 'degraded':
      return 'warning'
    case 'noop':
      return 'neutral'
    case 'syncing':
      return 'primary'
    default:
      return 'neutral'
  }
})

// "connected" on a repository record only means the last fetch succeeded,
// not that a live connection is held open - relabel it so the badge doesn't
// imply a persistent connection that doesn't exist.
const statusLabel = computed(() => {
  if (props.status?.toLowerCase() === 'connected') return 'Up to date'
  return props.status
})

const statusIcon = computed(() => {
  switch (props.status?.toLowerCase()) {
    case 'active':
    case 'success':
    case 'connected':
    case 'running':
      return 'i-lucide-check-circle-2'
    case 'error':
    case 'exited':
      return 'i-lucide-x-circle'
    case 'paused':
      return 'i-lucide-pause-circle'
    case 'pending':
    case 'queued':
      return 'i-lucide-clock'
    case 'stalled':
    case 'degraded':
      return 'i-lucide-alert-circle'
    case 'noop':
      return 'i-lucide-minus-circle'
    case 'syncing':
      return 'i-lucide-refresh-cw'
    default:
      return 'i-lucide-circle'
  }
})
</script>

<template>
  <UBadge
    v-if="mobileIconOnly"
    v-bind="$attrs"
    :color="statusColor"
    :icon="statusIcon"
    size="sm"
    variant="outline"
    class="sm:hidden"
    :aria-label="statusLabel"
    :title="statusLabel"
  />
  <UBadge
    v-bind="$attrs"
    :color="statusColor"
    :label="statusLabel"
    size="sm"
    variant="outline"
    :class="['uppercase', mobileIconOnly ? 'hidden sm:inline-flex' : '']"
  />
</template>
