<script setup lang="ts">
const props = withDefaults(
  defineProps<{
    label?: string
    ariaLabel?: string
    disabled?: boolean
    icon?: string
    size?: 'sm' | 'md'
  }>(),
  {
    label: undefined,
    ariaLabel: undefined,
    disabled: false,
    icon: undefined,
    size: 'md',
  }
)

defineEmits<{
  (e: 'click', event: MouseEvent): void
}>()
</script>

<template>
  <button
    type="button"
    class="inline-flex items-center justify-center gap-1.5 border border-gray-300 dark:border-carbon-800 rounded-lg bg-white dark:bg-carbon-950/70 hover:border-yellow-400/60 hover:bg-gray-50 dark:hover:bg-carbon-900 focus-visible:border-yellow-400/60 focus-visible:ring-1 focus-visible:ring-yellow-400/40 focus-visible:outline-hidden transition-all duration-200 shrink-0 disabled:cursor-not-allowed disabled:opacity-75 disabled:bg-gray-200 dark:disabled:bg-carbon-900"
    :class="props.size === 'sm' ? 'px-1.5 min-h-[26px] min-w-[26px]' : 'px-2.5 min-h-[38px]'"
    :disabled="disabled"
    :aria-label="ariaLabel"
    @click="$emit('click', $event)"
  >
    <UIcon
      v-if="icon"
      :name="icon"
      class="shrink-0"
      :class="props.size === 'sm' ? 'w-3.5 h-3.5 text-gray-600 dark:text-wire-200/70' : 'w-4 h-4 text-gray-400 dark:text-wire-200/30'"
    />
    <span v-if="label" class="flex-1 min-w-0 text-left truncate text-base sm:text-sm text-gray-900/90 dark:text-white/90">{{ label }}</span>
    <slot name="trailing" />
  </button>
</template>
