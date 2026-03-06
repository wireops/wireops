<script setup lang="ts">
const props = defineProps<{
  label?: string
  color?: string
  icon?: string
}>()
</script>

<template>
  <UBadge :color="(color as any) || 'primary'" size="sm" variant="outline">
    <div v-if="icon || $slots.default" class="flex items-center gap-1">
      <UIcon v-if="icon" :name="icon" class="w-3 h-3" />
      <span v-if="label">{{ label }}</span>
      <slot v-else />
    </div>
    <span v-else>{{ label }}</span>
  </UBadge>
</template>
