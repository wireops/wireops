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
        <UIcon name="i-lucide-shield-alert" class="w-5 h-5 text-red-500" />
        <h2 class="font-semibold text-gray-900 dark:text-wire-200 text-base">
          Apply Strict Production Preset
        </h2>
      </div>
    </template>

    <div class="space-y-3 text-sm text-gray-500 dark:text-wire-200/60">
      <p>
        This enables enforcement and turns on every container-isolation restriction (privileged mode, host network, host PID, host IPC, and Docker socket mounts) for all workers.
      </p>
      <p class="text-xs text-red-400">
        Existing stacks that rely on any of these are blocked from deploying until updated. This applies immediately to all workers.
      </p>
    </div>

    <template #footer>
      <div class="flex justify-end gap-2">
        <UButton label="Cancel" variant="outline" color="neutral" @click="emit('cancel')" />
        <UButton
          label="Apply Preset"
          color="error"
          icon="i-lucide-shield-alert"
          :loading="loading"
          @click="emit('confirm')"
        />
      </div>
    </template>
  </UCard>
</template>
