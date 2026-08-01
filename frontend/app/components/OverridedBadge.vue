<script setup lang="ts">
// Compact "this is running with manual render overrides, not what's in Git"
// indicator — a pencil icon + short label with a hover popover for the
// full explanation, used on StackCard (and anywhere else a stack/resource
// has render_overrides set).
withDefaults(defineProps<{
  label?: string
  description?: string
  icon?: string
}>(), {
  label: 'Override',
  description: "Overridden Values — running with manual render overrides, not what's in Git",
  icon: 'i-lucide-pencil-line',
})
</script>

<template>
  <UPopover mode="hover" :content="{ side: 'top' }">
    <span
      class="inline-flex items-center gap-1 shrink-0 rounded-md border border-amber-500/40 px-1.5 py-0.5 text-amber-500"
      :title="description"
      @click.stop.prevent
    >
      <UIcon :name="icon" class="h-3 w-3 shrink-0" aria-hidden="true" />
      <span class="text-xs font-medium">{{ label }}</span>
    </span>
    <template #content>
      <p class="px-2.5 py-1.5 text-xs text-gray-700 dark:text-wire-200">
        {{ description }}
      </p>
    </template>
  </UPopover>
</template>
