<script setup lang="ts">
import type { ButtonProps } from '@nuxt/ui/components/Button.vue'

withDefaults(defineProps<{
  icon: string
  label?: string
  color?: ButtonProps['color']
  variant?: ButtonProps['variant']
  size?: ButtonProps['size']
  disabled?: boolean
  loading?: boolean
  glow?: boolean
}>(), {
  label: undefined,
  color: 'primary',
  variant: 'solid',
  size: 'md',
  disabled: false,
  loading: false,
  glow: true,
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
    :class="variant === 'solid' && glow ? 'shadow-[0_0_16px_rgba(255,198,0,0.35)] transition-shadow hover:shadow-[0_0_24px_rgba(255,198,0,0.55)]' : ''"
    @click="$emit('click', $event)"
  />
</template>
