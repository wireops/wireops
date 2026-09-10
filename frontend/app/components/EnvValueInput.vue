<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'

const props = withDefaults(defineProps<{
  modelValue: string
  type?: string
  disabled?: boolean
  readonly?: boolean
  placeholder?: string
  icon?: string
  avatar?: { src: string } | null
  title?: string
  ariaLabel?: string
}>(), { type: 'text', placeholder: 'value', icon: undefined, avatar: null, title: undefined, ariaLabel: undefined })
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const expanded = ref(false)
const original = ref('')
const textarea = ref<HTMLTextAreaElement | null>(null)
const locked = computed(() => props.disabled || props.readonly)
const multiline = computed(() => /[\r\n]/.test(props.modelValue))
const preview = computed(() => props.modelValue.replace(/\r\n|\r|\n/g, ' ↵ '))

async function expand() {
  original.value = props.modelValue
  expanded.value = true
  await nextTick()
  textarea.value?.focus()
}

function cancel() {
  if (!locked.value) emit('update:modelValue', original.value)
  expanded.value = false
  original.value = ''
}

function close() {
  expanded.value = false
  original.value = ''
}

function paste(event: ClipboardEvent) {
  if (locked.value || expanded.value) return
  const text = event.clipboardData?.getData('text/plain') ?? ''
  if (!/[\r\n]/.test(text)) return
  event.preventDefault()
  const input = event.target as HTMLInputElement
  const start = input.selectionStart ?? props.modelValue.length
  const end = input.selectionEnd ?? start
  void expand()
  emit('update:modelValue', props.modelValue.slice(0, start) + text + props.modelValue.slice(end))
}

// Hiding a revealed secret must discard both the editor and its old snapshot.
watch(() => props.type, () => close())
</script>

<template>
  <div class="w-full min-w-0 space-y-2" @paste.capture="paste">
    <AppTextInput
      v-if="!expanded"
      :model-value="preview"
      :type="type"
      :disabled="disabled"
      :readonly="readonly || multiline"
      :placeholder="placeholder"
      :icon="icon"
      :avatar="avatar"
      :title="title"
      :aria-label="ariaLabel"
      @update:model-value="emit('update:modelValue', $event)"
      @focus="multiline && !locked && expand()"
    >
      <template #trailing>
        <slot name="trailing" />
        <button
          v-if="!locked || (multiline && type !== 'password')"
          type="button"
          aria-label="Expand value"
          title="Expand value"
          class="shrink-0 p-1 text-gray-500 hover:text-yellow-500"
          @click="expand"
        >
          <UIcon name="i-lucide-expand" class="h-4 w-4" />
        </button>
      </template>
    </AppTextInput>
    <template v-else>
      <textarea
        ref="textarea"
        :value="modelValue"
        :readonly="locked"
        :placeholder="placeholder"
        :aria-label="ariaLabel || 'Environment variable value'"
        rows="8"
        spellcheck="false"
        autocomplete="off"
        class="block w-full min-w-0 resize-y rounded-lg border border-gray-300 bg-white p-2 font-mono text-base focus:border-yellow-400 focus:outline-hidden sm:text-sm dark:border-carbon-800 dark:bg-carbon-950"
        @input="emit('update:modelValue', ($event.target as HTMLTextAreaElement).value)"
        @keydown.esc.stop.prevent="cancel"
      />
      <p v-if="!locked" class="text-xs text-gray-500">Paste the original value. Line breaks are preserved; no extra quotes or escaping needed.</p>
      <div class="flex flex-wrap justify-end gap-2">
        <slot name="trailing" />
        <button v-if="!locked" type="button" class="text-sm text-gray-500" @click="cancel">Cancel</button>
        <button type="button" class="text-sm text-yellow-600 dark:text-yellow-400" @click="close">{{ locked ? 'Close' : 'Done' }}</button>
      </div>
    </template>
  </div>
</template>
