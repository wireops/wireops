<script setup lang="ts">
withDefaults(
  defineProps<{
    modelValue: string
    placeholder?: string
    id?: string
    ariaLabel?: string
    disabled?: boolean
    readonly?: boolean
    rows?: number
  }>(),
  {
    placeholder: '',
    id: undefined,
    ariaLabel: undefined,
    disabled: false,
    readonly: false,
    rows: 4,
  }
)

defineEmits<{
  (e: 'update:modelValue', value: string): void
  (e: 'focus' | 'blur', event: FocusEvent): void
  (e: 'keyup', event: KeyboardEvent): void
}>()
</script>

<template>
  <div
    class="flex px-2.5 py-2 border border-gray-200 dark:border-carbon-800 rounded-lg bg-white dark:bg-carbon-950/70 focus-within:border-yellow-400/60 focus-within:ring-1 focus-within:ring-yellow-400/40 transition-all duration-200 w-full"
    :class="disabled ? 'opacity-60' : ''"
  >
    <textarea
      :id="id"
      :rows="rows"
      class="flex-1 min-w-0 bg-transparent border-0 p-0 resize-y focus:ring-0 focus:outline-hidden text-base sm:text-sm text-gray-900/90 dark:text-white/90 placeholder-gray-400 dark:placeholder-wire-200/30 disabled:cursor-not-allowed"
      :placeholder="placeholder"
      :aria-label="ariaLabel"
      :value="modelValue"
      :disabled="disabled"
      :readonly="readonly"
      @input="$emit('update:modelValue', ($event.target as HTMLTextAreaElement).value)"
      @focus="$emit('focus', $event)"
      @blur="$emit('blur', $event)"
      @keyup="$emit('keyup', $event)"
    />
  </div>
</template>
