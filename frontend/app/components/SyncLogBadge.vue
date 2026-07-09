<script setup lang="ts">
const props = defineProps<{
  trigger: string
  status: string
}>()

const triggerIcon = computed(() => {
  switch (props.trigger) {
    case 'webhook': return 'i-lucide-webhook'
    case 'cron': return 'i-lucide-clock'
    case 'manual': return 'i-lucide-play'
    case 'transfer': return 'i-lucide-arrow-right-left'
    case 'rollback': return 'i-lucide-undo-2'
    case 'redeploy': return 'i-lucide-recycle'
    case 'queue': return 'i-lucide-list-todo'
    default: return 'i-lucide-zap'
  }
})

const statusIcon = computed(() => {
  if (props.status === 'noop') return 'i-lucide-minus-circle'
  return triggerIcon.value
})

const statusColor = computed(() => {
  switch (props.status) {
    case 'success': return 'success'
    case 'syncing': case 'running': return 'primary'
    case 'error': return 'error'
    case 'pending': case 'queued': return 'warning'
    case 'noop': return 'neutral'
    default: return 'neutral'
  }
})

const label = computed(() => props.status === 'noop' ? 'no-op' : props.trigger)
</script>

<template>
  <UBadge :color="statusColor" size="sm" variant="outline" class="uppercase">
    <div class="flex items-center gap-1">
      <UIcon :name="statusIcon" class="w-3 h-3" />
      <span>{{ label }}</span>
    </div>
  </UBadge>
</template>
