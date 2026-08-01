<script setup lang="ts">
withDefaults(
  defineProps<{
    label?: string
    ariaLabel?: string
    disabled?: boolean
    icon?: string
  }>(),
  {
    label: undefined,
    ariaLabel: undefined,
    disabled: false,
    icon: undefined,
  }
)

defineEmits<{
  (e: 'click', event: MouseEvent): void
}>()
</script>

<template>
  <button
    type="button"
    class="inline-flex items-center gap-1.5 px-2.5 border border-gray-200 dark:border-carbon-800 rounded-lg bg-white dark:bg-carbon-950/70 hover:border-yellow-400/60 focus-visible:border-yellow-400/60 focus-visible:ring-1 focus-visible:ring-yellow-400/40 focus-visible:outline-hidden transition-all duration-200 min-h-[38px] shrink-0 disabled:cursor-not-allowed disabled:opacity-60"
    :disabled="disabled"
    :aria-label="ariaLabel"
    @click="$emit('click', $event)"
  >
    <UIcon v-if="icon" :name="icon" class="w-4 h-4 text-gray-400 dark:text-wire-200/30 shrink-0" />
    <span v-if="label" class="flex-1 min-w-0 text-left truncate text-base sm:text-sm text-gray-900/90 dark:text-white/90">{{ label }}</span>
    <slot name="trailing" />
  </button>
</template>
