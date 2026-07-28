<script setup lang="ts">
const props = withDefaults(defineProps<{
  ariaLabel?: string
  size?: string
  class?: string
}>(), {
  ariaLabel: 'Close',
  size: 'md',
  class: '',
})

defineEmits<{ click: [MouseEvent] }>()
</script>

<template>
  <UButton
    color="neutral"
    variant="ghost"
    icon="i-lucide-x"
    :size="size"
    :aria-label="ariaLabel"
    :class="props.class"
    @click="$emit('click', $event)"
  />
</template>
