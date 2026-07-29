<script setup lang="ts">
withDefaults(defineProps<{
  icon: string
  title: string
  description: string
  ctaLabel?: string
  ctaIcon?: string
}>(), {
  ctaLabel: undefined,
  ctaIcon: 'i-lucide-plus',
})

defineEmits<{ cta: [] }>()
</script>

<template>
  <div class="text-center py-12">
    <div class="w-14 h-14 rounded-full bg-wire-400/10 border border-wire-400/20 flex items-center justify-center mx-auto mb-3">
      <UIcon :name="icon" class="w-7 h-7 text-wire-400" />
    </div>
    <h3 class="text-lg font-medium text-gray-900 dark:text-wire-200 mb-1">{{ title }}</h3>
    <p class="text-gray-500 dark:text-wire-200/50 text-sm">{{ description }}</p>
    <ActionButton v-if="ctaLabel" :icon="ctaIcon" :label="ctaLabel" size="sm" class="mt-4" @click="$emit('cta')" />
  </div>
</template>
