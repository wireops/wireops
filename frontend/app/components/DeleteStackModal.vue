<script setup lang="ts">
import { computed, ref } from 'vue'

const { deleteStack } = useApi()
const toast = useToast()
const { announce } = useA11yAnnouncer()

const props = defineProps<{
  stack: any // the stack record (must have .id, .name, and optionally .expand.worker)
  workerOffline?: boolean
}>()

const emit = defineEmits<{
  deleted: []
  cancel: []
}>()

const showForceDeleteOption = ref(false)

// Worker connectivity check
const workerOffline = computed(() => {
  if (props.workerOffline) return true
  if (showForceDeleteOption.value) return true
  const worker = props.stack?.expand?.worker
  if (!worker) return true
  // A worker is considered offline if its status is not ACTIVE
  if (worker.status && worker.status !== WORKER_STATUS.ACTIVE) return true
  return false
})

const workerName = computed(() => props.stack?.expand?.worker?.hostname || 'Assigned worker')

const forceDelete = ref(false)
const deleting = ref(false)
const errorMsg = ref('')

async function confirmDelete() {
  if (workerOffline.value && !forceDelete.value) return
  deleting.value = true
  errorMsg.value = ''
  try {
    const res = await deleteStack(props.stack.id, forceDelete.value)
    if (res?.error) {
      errorMsg.value = res.error
      if (res.error.toLowerCase().includes('offline')) {
        showForceDeleteOption.value = true
      }
      return
    }
    toast.add({ title: `Stack "${props.stack.name}" deleted`, color: 'success' })
    announce(`Stack ${props.stack.name} deleted`)
    emit('deleted')
  } catch (e: any) {
    errorMsg.value = e?.message || 'Unexpected error'
    if (errorMsg.value.toLowerCase().includes('offline')) {
      showForceDeleteOption.value = true
    }
    announce(`Failed to delete stack ${props.stack?.name || ''}`.trim(), 'assertive')
  } finally {
    deleting.value = false
  }
}
</script>

<template>
  <UCard>
    <template #header>
      <div class="flex items-center gap-2">
        <UIcon name="i-lucide-trash-2" class="w-5 h-5 text-red-500" />
        <h2 class="font-semibold">Delete Stack</h2>
      </div>
    </template>

    <div class="space-y-4">
      <!-- Worker offline warning -->
      <div v-if="workerOffline" class="flex items-start gap-3 rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3">
        <UIcon name="i-lucide-wifi-off" class="w-5 h-5 text-red-500 mt-0.5 shrink-0" />
        <div class="space-y-2">
          <div>
            <p class="text-sm font-medium text-red-500">Worker unavailable</p>
            <p class="text-xs text-red-500 mt-1 leading-relaxed">
              <WorkerNameLabel :name="workerName" /> is offline. You can force delete the database records, but the containers on the host will become orphaned.
            </p>
          </div>
          <div class="pt-2">
            <UCheckbox
              v-model="forceDelete"
              color="error"
              label="Force delete database records only"
              :ui="{
                root: 'items-center',
                base: 'border border-red-500 dark:border-red-400/80 !ring-0',
                indicator: '!bg-transparent',
                icon: 'text-red-500 dark:text-red-400',
                label: 'text-red-600 dark:text-red-400 font-medium text-xs'
              }"
            />
          </div>
        </div>
      </div>

      <!-- Confirmation text -->
      <div v-else class="text-sm text-gray-500 space-y-1">
        <p>Are you sure you want to delete <span class="font-semibold text-gray-800 dark:text-gray-200">{{ stack?.name }}</span>?</p>
        <p class="text-xs">The worker will run <code class="bg-gray-100 dark:bg-gray-800 px-1 rounded">docker compose down</code> to stop all containers before removing the stack.</p>
        <p class="text-xs text-red-500 font-medium">This action cannot be undone.</p>
      </div>

      <!-- API error -->
      <div v-if="errorMsg" class="flex items-start gap-3 rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3" role="alert" aria-live="assertive">
        <UIcon name="i-lucide-circle-x" class="w-5 h-5 text-red-500 mt-0.5 shrink-0" />
        <p class="text-sm text-red-500">{{ errorMsg }}</p>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-2">
        <UButton label="Cancel" variant="outline" @click="emit('cancel')" />
        <UButton
          label="Delete Stack"
          color="error"
          icon="i-lucide-trash-2"
          :loading="deleting"
          :disabled="workerOffline && !forceDelete"
          @click="confirmDelete"
        />
      </div>
    </template>
  </UCard>
</template>
