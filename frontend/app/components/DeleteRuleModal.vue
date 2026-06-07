<script setup lang="ts">
const props = defineProps<{
  value: string
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
        <UIcon name="i-lucide-trash-2" class="w-5 h-5 text-red-500" />
        <h2 class="font-semibold text-red-500 text-base">Remove Security Rule</h2>
      </div>
    </template>
    
    <div class="space-y-3 text-sm text-gray-500 dark:text-wire-200/60">
      <p>
        Are you sure you want to remove this allowed pattern?
      </p>
      <div class="p-3 bg-gray-50 dark:bg-carbon-900 rounded-lg border border-gray-100 dark:border-carbon-800 font-mono text-xs text-gray-900 dark:text-wire-200 break-all">
        {{ value }}
      </div>
      <p class="text-xs text-red-400">
        Removing this rule will immediately update the active security policies.
      </p>
    </div>
    
    <template #footer>
      <div class="flex justify-end gap-2">
        <UButton label="Cancel" variant="outline" color="neutral" @click="emit('cancel')" />
        <UButton
          label="Remove Rule"
          color="error"
          icon="i-lucide-trash-2"
          @click="emit('confirm')"
        />
      </div>
    </template>
  </UCard>
</template>
