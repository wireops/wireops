<script setup lang="ts">
defineProps<{
  actions: {
    key: string
    label: string
    description: string
    buttonLabel: string
    icon?: string
    color?: 'error' | 'warning'
    onClick: () => void
  }[]
}>()

const open = defineModel<boolean>('open', { default: false })
</script>

<template>
  <AccordionCard v-model:open="open" title="Danger Zone" icon="i-lucide-triangle-alert" icon-class="text-red-500" title-class="text-red-500" chevron-class="text-red-500">
    <div class="space-y-4">
      <template v-for="(action, index) in actions" :key="action.key">
        <hr v-if="index > 0" class="border-gray-200 dark:border-carbon-700">
        <div class="flex items-center justify-between">
          <div>
            <p class="text-sm font-medium">{{ action.label }}</p>
            <p class="text-xs text-gray-500">{{ action.description }}</p>
          </div>
          <UButton
            :label="action.buttonLabel"
            :color="action.color || 'error'"
            variant="outline"
            size="sm"
            :icon="action.icon"
            @click="action.onClick"
          />
        </div>
      </template>
    </div>
  </AccordionCard>
</template>
