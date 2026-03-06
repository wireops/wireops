<script setup lang="ts">
const props = defineProps<{
  status: string
}>()

const statusColor = computed(() => {
  switch (props.status?.toLowerCase()) {
    case 'active':
    case 'success':
    case 'connected':
    case 'done':
    case 'running':
      return 'success'
    case 'error':
    case 'exited':
      return 'error'
    case 'paused':
    case 'pending':
    case 'queued':
      return 'warning'
    case 'syncing':
      return 'primary'
    default:
      return 'neutral'
  }
})
</script>

<template>
  <UBadge :color="statusColor" :label="status" size="sm" variant="outline" class="uppercase" />
</template>
