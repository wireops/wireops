<script setup lang="ts">
withDefaults(defineProps<{
  icon: string
  label?: string
  color?: string
  variant?: string
  size?: string
  disabled?: boolean
  loading?: boolean
}>(), {
  label: undefined,
  color: 'primary',
  variant: 'solid',
  size: 'md',
  disabled: false,
  loading: false,
})

defineEmits<{ click: [MouseEvent] }>()
</script>

<template>
  <UButton
    :icon="icon"
    :label="label"
    :color="color"
    :variant="variant"
    :size="size"
    :disabled="disabled"
    :loading="loading"
    class="shadow-[0_0_16px_rgba(255,198,0,0.35)] transition-shadow hover:shadow-[0_0_24px_rgba(255,198,0,0.55)]"
    @click="$emit('click', $event)"
  />
</template>
