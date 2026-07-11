<script setup lang="ts">
withDefaults(
  defineProps<{
    modelValue: string
    placeholder?: string
    id?: string
    ariaLabel?: string
  }>(),
  {
    placeholder: '',
    id: undefined,
    ariaLabel: undefined,
  }
)

defineEmits<{
  (e: 'update:modelValue', value: string): void
}>()
</script>

<template>
  <div class="flex items-center px-2.5 border border-gray-200 dark:border-carbon-800 rounded-lg bg-white dark:bg-carbon-950/70 focus-within:border-yellow-400/60 focus-within:ring-1 focus-within:ring-yellow-400/40 transition-all duration-200 w-full h-[38px]">
    <input
      :id="id"
      type="text"
      class="flex-1 min-w-0 bg-transparent border-0 p-0 focus:ring-0 focus:outline-hidden text-sm h-6 text-gray-900/90 dark:text-white/90 placeholder-gray-400 dark:placeholder-wire-200/30"
      :placeholder="placeholder"
      :aria-label="ariaLabel"
      :value="modelValue"
      @input="$emit('update:modelValue', ($event.target as HTMLInputElement).value)"
    >
  </div>
</template>
