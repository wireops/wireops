<script setup lang="ts">
import { computed, ref, useAttrs } from 'vue'

defineOptions({ inheritAttrs: false })

const props = withDefaults(
  defineProps<{
    label?: boolean
    loading?: boolean
    size?: 'xs' | 'sm' | 'md' | 'lg' | 'xl'
  }>(),
  {
    label: false,
    loading: false,
    size: 'xs',
  }
)

const attrs = useAttrs()
const spinning = ref(false)
const isSpinning = computed(() => props.loading || spinning.value)

const MIN_SPIN_MS = 400

async function onClick(event: MouseEvent) {
  spinning.value = true
  const startedAt = Date.now()
  try {
    const handler = attrs.onClick as ((e: MouseEvent) => unknown) | undefined
    await handler?.(event)
  } finally {
    const elapsed = Date.now() - startedAt
    if (elapsed < MIN_SPIN_MS) {
      await new Promise((resolve) => setTimeout(resolve, MIN_SPIN_MS - elapsed))
    }
    spinning.value = false
  }
}
</script>

<template>
  <UTooltip :text="label ? undefined : 'Refresh'">
    <UButton
      icon="i-lucide-refresh-cw"
      :label="label ? 'Refresh' : undefined"
      :aria-label="label ? undefined : 'Refresh'"
      variant="ghost"
      :size="size"
      color="neutral"
      :disabled="isSpinning"
      :ui="{ leadingIcon: isSpinning ? 'animate-spin' : '' }"
      @click="onClick"
    />
  </UTooltip>
</template>
