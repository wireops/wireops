<script setup lang="ts">
import type { ButtonProps } from '@nuxt/ui/components/Button.vue'

withDefaults(defineProps<{
  label?: string
  size?: ButtonProps['size']
  disabled?: boolean
  block?: boolean
  type?: 'button' | 'submit' | 'reset'
}>(), {
  label: 'Cancel',
  size: 'md',
  disabled: false,
  block: false,
  type: 'button',
})

defineEmits<{ click: [MouseEvent] }>()
</script>

<template>
  <UButton
    :type="type"
    :label="label"
    :size="size"
    :disabled="disabled"
    :block="block"
    variant="outline"
    color="neutral"
    @click="$emit('click', $event)"
  />
</template>
