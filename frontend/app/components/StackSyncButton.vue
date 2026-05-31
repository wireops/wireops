<script setup lang="ts">
defineProps<{
  canSync: boolean
  disabledReason?: string
}>()

defineEmits<{
  click: []
}>()
</script>

<template>
  <UTooltip :text="canSync ? 'Sync now' : (disabledReason ?? 'Sync unavailable')">
    <UButton
      icon="i-lucide-refresh-cw"
      label="Sync Now"
      block
      :color="canSync ? 'primary' : 'neutral'"
      :variant="canSync ? 'solid' : 'outline'"
      class="shadow-[0_0_16px_rgba(255,198,0,0.35)] transition-shadow hover:shadow-[0_0_24px_rgba(255,198,0,0.55)] disabled:shadow-none"
      :disabled="!canSync"
      @click="$emit('click')"
    />
  </UTooltip>
</template>
