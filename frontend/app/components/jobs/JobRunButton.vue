<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  enabled?: boolean
  hasError?: boolean
  loading?: boolean
}>()

const emit = defineEmits<{
  click: []
}>()

const isDisabled = computed(() => !props.enabled || props.hasError)

const variant = computed(() => props.hasError ? 'outline' : 'solid')
const color = computed(() => props.hasError ? 'neutral' : 'primary')
</script>

<template>
  <UButton
    icon="i-lucide-play"
    label="Run now"
    :loading="props.loading"
    :disabled="isDisabled"
    :variant="variant"
    :color="color"
    @click="emit('click')"
  />
</template>
