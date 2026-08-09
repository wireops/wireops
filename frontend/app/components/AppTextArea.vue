<script setup lang="ts">
import { useFormField } from '@nuxt/ui/composables/useFormField'

const props = withDefaults(
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

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
  (e: 'focus' | 'blur', event: FocusEvent): void
  (e: 'keyup', event: KeyboardEvent): void
}>()

const { id: fieldId, name: fieldName, disabled: fieldDisabled, ariaAttrs, emitFormInput, emitFormFocus, emitFormBlur } = useFormField(props)

function onInput(event: Event) {
  emit('update:modelValue', (event.target as HTMLTextAreaElement).value)
  emitFormInput()
}

function onFocus(event: FocusEvent) {
  emit('focus', event)
  emitFormFocus()
}

function onBlur(event: FocusEvent) {
  emit('blur', event)
  emitFormBlur()
}
</script>

<template>
  <div
    class="flex px-2.5 py-2 border border-gray-300 dark:border-carbon-800 rounded-lg focus-within:border-yellow-400/60 focus-within:ring-1 focus-within:ring-yellow-400/40 transition-all duration-200 w-full"
    :class="(fieldDisabled ?? disabled) ? 'bg-gray-200 dark:bg-carbon-900 opacity-75' : 'bg-white dark:bg-carbon-950/70'"
  >
    <textarea
      :id="fieldId"
      :name="fieldName"
      :rows="rows"
      class="flex-1 min-w-0 bg-transparent border-0 p-0 resize-y focus:ring-0 focus:outline-hidden text-base sm:text-sm text-gray-900/90 dark:text-white/90 placeholder-gray-400 dark:placeholder-wire-200/30 disabled:cursor-not-allowed"
      :placeholder="placeholder"
      :aria-label="ariaLabel"
      v-bind="ariaAttrs"
      :value="modelValue"
      :disabled="fieldDisabled ?? disabled"
      :readonly="readonly"
      @input="onInput"
      @focus="onFocus"
      @blur="onBlur"
      @keyup="$emit('keyup', $event)"
    />
  </div>
</template>
