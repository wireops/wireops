<script setup lang="ts">
const props = defineProps<{
  title: string
  description: string
  confirmLabel?: string
  confirmColor?: string
  loading?: boolean
}>()

const emit = defineEmits<{
  confirm: []
  cancel: []
}>()

const isOpen = defineModel<boolean>('open', { default: false })
</script>

<template>
  <UModal v-model:open="isOpen">
    <template #content>
      <UCard>
        <template #header>
          <div class="flex items-center gap-2" :class="confirmColor === 'error' ? 'text-red-600' : 'text-amber-500'">
            <UIcon name="i-lucide-alert-triangle" class="w-5 h-5" />
            <h3 class="font-semibold text-gray-900 dark:text-white">{{ title }}</h3>
          </div>
        </template>
        
        <div class="space-y-4">
          <p class="text-sm text-gray-500 dark:text-gray-400">
            {{ description }}
          </p>
        </div>

        <template #footer>
          <div class="flex justify-end gap-2">
            <UButton label="Cancel" variant="outline" color="neutral" @click="isOpen = false; emit('cancel')" />
            <UButton 
              :color="confirmColor || 'primary'" 
              :label="confirmLabel || 'Confirm'" 
              :loading="loading" 
              @click="emit('confirm')" 
            />
          </div>
        </template>
      </UCard>
    </template>
  </UModal>
</template>
