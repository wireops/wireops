<script setup lang="ts">
const { revokeWorker } = useApi()
const toast = useToast()

const props = defineProps<{
  worker: any // the worker record
}>()

const emit = defineEmits<{
  revoked: []
  cancel: []
}>()

const revoking = ref(false)
const errorMsg = ref('')

async function confirmRevoke() {
  revoking.value = true
  errorMsg.value = ''
  try {
    await revokeWorker(props.worker.id)
    toast.add({ title: `Worker "${props.worker.hostname}" revoked successfully`, color: 'success' })
    emit('revoked')
  } catch (e: any) {
    errorMsg.value = e?.message || 'Unexpected error'
  } finally {
    revoking.value = false
  }
}
</script>

<template>
  <UCard>
    <template #header>
        <div class="flex items-center gap-2">
          <UIcon name="i-lucide-trash-2" class="w-5 h-5 text-red-500" />
          <h2 class="font-semibold text-lg text-red-500">Revoke Worker</h2>
        </div>
      </template>

      <div class="space-y-4">
        <div class="text-sm text-gray-500 space-y-1">
          <p>Are you sure you want to revoke the worker <span class="font-semibold text-gray-800 dark:text-gray-200">{{ worker?.hostname }}</span>?</p>
          <p class="text-xs">All active client certificates for this worker will be invalidated, dropping its communication to the wireops hub.</p>
          <p class="text-xs text-red-500 font-medium">This action cannot be undone.</p>
        </div>

        <!-- API error -->
        <div v-if="errorMsg" class="flex items-start gap-3 rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3">
          <UIcon name="i-lucide-circle-x" class="w-5 h-5 text-red-500 mt-0.5 shrink-0" />
          <p class="text-sm text-red-500">{{ errorMsg }}</p>
        </div>
      </div>

      <template #footer>
        <div class="flex justify-end gap-2">
          <UButton label="Cancel" variant="outline" @click="emit('cancel')" />
          <UButton
            label="Revoke Worker"
            color="red"
            icon="i-lucide-trash-2"
            :loading="revoking"
            @click="confirmRevoke"
          />
        </div>
    </template>
  </UCard>
</template>
