<script setup lang="ts">
const props = defineProps<{
  status: string
}>()

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
</script>

<template>
  <UBadge :color="statusColor" :label="statusLabel" size="sm" variant="outline" class="uppercase" />
</template>
