<script setup lang="ts">
import type { ButtonProps } from '@nuxt/ui/components/Button.vue'

const props = withDefaults(defineProps<{
  ariaLabel?: string
  size?: ButtonProps['size']
  class?: string
  icon?: string
  label?: string
  variant?: ButtonProps['variant']
}>(), {
  ariaLabel: 'Close',
  size: 'md',
  class: '',
  icon: 'i-lucide-x',
  label: undefined,
  variant: 'ghost',
})

defineEmits<{ click: [MouseEvent] }>()
</script>

<template>
  <UButton
    color="neutral"
    :variant="variant"
    :icon="icon"
    :label="label"
    :size="size"
    :aria-label="ariaLabel"
    :class="props.class"
    @click="$emit('click', $event)"
  />
</template>
