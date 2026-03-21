<script setup lang="ts">
const { $pb } = useNuxtApp()
const { transferStack } = useApi()
const toast = useToast()

const props = defineProps<{
  stack: any // the stack record (must have .id, .name, .worker)
}>()

const emit = defineEmits<{
  transferred: []
  cancel: []
}>()

const workers = ref<any[]>([])
const selectedWorkerId = ref<string>('')
const transferring = ref(false)
const errorMsg = ref('')

const workerOptions = computed(() =>
  workers.value
    .filter((a: any) => a.id !== props.stack?.worker)
    .map((a: any) => ({ label: a.hostname, value: a.id }))
)

onMounted(async () => {
  try {
    workers.value = await $pb.collection('workers').getFullList({
      filter: 'status = "ACTIVE"',
      sort: 'hostname',
    })
  } catch {
    workers.value = []
  }
})

async function confirmTransfer() {
  if (!selectedWorkerId.value) return
  transferring.value = true
  errorMsg.value = ''
  try {
    await transferStack(props.stack.id, selectedWorkerId.value)
    const targetWorker = workers.value.find((a) => a.id === selectedWorkerId.value)
    toast.add({
      title: `Stack "${props.stack.name}" transfer started`,
      description: `Provisioning on ${targetWorker?.hostname || 'target worker'} — this may take a moment.`,
      color: 'warning',
    })
    emit('transferred')
  } catch (e: any) {
    errorMsg.value = e?.message || 'Unexpected error'
  } finally {
    transferring.value = false
  }
}
</script>

<template>
  <UCard>
    <template #header>
      <div class="flex items-center gap-2">
        <UIcon name="i-lucide-arrow-right-left" class="w-5 h-5 text-warning-500" />
        <h2 class="font-semibold">Transfer Stack</h2>
      </div>
    </template>

    <div class="space-y-4">
      <!-- Persistence warning -->
      <UAlert
        color="warning"
        icon="i-lucide-triangle-alert"
        title="Data will not be preserved"
      description="Transferring between workers does not maintain volumes, container state, or any local storage. Plan your backup and restoration before proceeding."
    />

      <!-- Worker selector -->
      <UFormField label="Target Worker" :help="workerOptions.length === 0 ? 'No other active workers available.' : undefined">
        <USelect
          v-model="selectedWorkerId"
          :items="workerOptions"
          placeholder="Select a target worker"
          :disabled="workerOptions.length === 0"
          class="w-full"
        />
      </UFormField>

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
          label="Transfer Stack"
          color="warning"
          icon="i-lucide-arrow-right-left"
          :loading="transferring"
          :disabled="!selectedWorkerId || workerOptions.length === 0"
          @click="confirmTransfer"
        />
      </div>
    </template>
  </UCard>
</template>
