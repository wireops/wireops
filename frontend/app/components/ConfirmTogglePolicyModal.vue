<script setup lang="ts">
const props = defineProps<{
  enabled: boolean
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
        <UIcon
          :name="enabled ? 'i-lucide-shield-check' : 'i-lucide-shield-alert'"
          :class="['w-5 h-5', enabled ? 'text-yellow-500' : 'text-red-500']"
        />
        <h2 class="font-semibold text-gray-900 dark:text-wire-200 text-base">
          {{ enabled ? 'Enable' : 'Disable' }} Security Policies
        </h2>
      </div>
    </template>
    
    <div class="space-y-3 text-sm text-gray-500 dark:text-wire-200/60">
      <p v-if="enabled">
        Are you sure you want to enable global security policy enforcement?
      </p>
      <p v-else>
        Are you sure you want to disable global security policy enforcement?
      </p>
      
      <p v-if="enabled" class="text-xs text-gray-400">
        This will enforce image, volume, and network restrictions across all workers.
      </p>
      <p v-else class="text-xs text-red-400">
        Disabling enforcement means workers will not validate images, volumes, or networks during stack reconciliation or job runs. This may pose a security risk.
      </p>
    </div>
    
    <template #footer>
      <div class="flex justify-end gap-2">
        <UButton label="Cancel" variant="outline" color="neutral" @click="emit('cancel')" />
        <UButton
          :label="enabled ? 'Enable Policies' : 'Disable Policies'"
          :color="enabled ? 'warning' : 'error'"
          :icon="enabled ? 'i-lucide-shield-check' : 'i-lucide-shield-alert'"
          :loading="loading"
          @click="emit('confirm')"
        />
      </div>
    </template>
  </UCard>
</template>
