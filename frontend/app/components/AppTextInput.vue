<script setup lang="ts">
withDefaults(
  defineProps<{
    modelValue: string
    placeholder?: string
    id?: string
    ariaLabel?: string
    type?: string
    disabled?: boolean
    readonly?: boolean
    icon?: string
    avatar?: { src: string } | null
    title?: string
  }>(),
  {
    placeholder: '',
    id: undefined,
    ariaLabel: undefined,
    type: 'text',
    disabled: false,
    readonly: false,
    icon: undefined,
    avatar: null,
    title: undefined,
  }
)

defineEmits<{
  (e: 'update:modelValue', value: string): void
}>()
</script>

<template>
  <div
    class="flex items-center gap-1.5 px-2.5 border border-gray-200 dark:border-carbon-800 rounded-lg bg-white dark:bg-carbon-950/70 focus-within:border-yellow-400/60 focus-within:ring-1 focus-within:ring-yellow-400/40 transition-all duration-200 w-full h-[38px]"
    :class="disabled ? 'opacity-60' : ''"
  >
    <UIcon v-if="icon" :name="icon" class="w-4 h-4 text-gray-400 dark:text-wire-200/30 shrink-0" :title="title" />
    <img v-else-if="avatar" :src="avatar.src" :title="title" class="w-4 h-4 shrink-0 object-contain">
    <input
      :id="id"
      :type="type"
      class="flex-1 min-w-0 bg-transparent border-0 p-0 focus:ring-0 focus:outline-hidden text-sm h-6 text-gray-900/90 dark:text-white/90 placeholder-gray-400 dark:placeholder-wire-200/30 disabled:cursor-not-allowed"
      :placeholder="placeholder"
      :aria-label="ariaLabel"
      :value="modelValue"
      :disabled="disabled"
      :readonly="readonly"
      @input="$emit('update:modelValue', ($event.target as HTMLInputElement).value)"
    >
    <slot name="trailing" />
  </div>
</template>
