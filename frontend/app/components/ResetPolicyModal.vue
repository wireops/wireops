<script setup lang="ts">
const props = defineProps<{
  loading?: boolean
}>()

const emit = defineEmits<{
  confirm: []
  cancel: []
}>()
</script>

<template>
  <UCard>
    <template #header>
      <div class="flex items-center gap-2">
        <UIcon name="i-lucide-rotate-ccw" class="w-5 h-5 text-yellow-500" />
        <h2 class="font-semibold text-gray-900 dark:text-wire-200 text-base">Reset Policy to Defaults</h2>
      </div>
    </template>
    
    <div class="space-y-3 text-sm text-gray-500 dark:text-wire-200/60">
      <p>
        Are you sure you want to reset this worker's security policies to the global defaults?
      </p>
      <p class="text-xs text-gray-400">This will erase all custom image, volume, and network overrides for this worker.</p>
      <p class="text-xs text-yellow-500 font-medium">This action cannot be undone.</p>
    </div>
    
    <template #footer>
      <div class="flex justify-end gap-2">
        <UButton label="Cancel" variant="outline" color="neutral" @click="emit('cancel')" />
        <UButton
          label="Reset Policy"
          color="warning"
          icon="i-lucide-rotate-ccw"
          :loading="loading"
          @click="emit('confirm')"
        />
      </div>
    </template>
  </UCard>
</template>
