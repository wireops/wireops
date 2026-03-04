<script setup lang="ts">
const { $pb } = useNuxtApp()
const { transferStack } = useApi()
const toast = useToast()

const props = defineProps<{
  stack: any // the stack record (must have .id, .name, .agent)
}>()

const emit = defineEmits<{
  transferred: []
  cancel: []
}>()

const agents = ref<any[]>([])
const selectedAgentId = ref<string>('')
const transferring = ref(false)
const errorMsg = ref('')

const agentOptions = computed(() =>
  agents.value
    .filter((a: any) => a.id !== props.stack?.agent)
    .map((a: any) => ({ label: a.hostname, value: a.id }))
)

onMounted(async () => {
  try {
    agents.value = await $pb.collection('agents').getFullList({
      filter: 'status = "ACTIVE"',
      sort: 'hostname',
    })
  } catch {
    agents.value = []
  }
})

async function confirmTransfer() {
  if (!selectedAgentId.value) return
  transferring.value = true
  errorMsg.value = ''
  try {
    await transferStack(props.stack.id, selectedAgentId.value)
    const targetAgent = agents.value.find((a) => a.id === selectedAgentId.value)
    toast.add({
      title: `Stack "${props.stack.name}" transfer started`,
      description: `Provisioning on ${targetAgent?.hostname || 'target agent'} — this may take a moment.`,
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
        description="Transferring between agents does not maintain volumes, container state, or any local storage. Plan your backup and restoration before proceeding."
      />

      <!-- Agent selector -->
      <UFormField label="Target Agent" :help="agentOptions.length === 0 ? 'No other active agents available.' : undefined">
        <USelect
          v-model="selectedAgentId"
          :items="agentOptions"
          placeholder="Select a target agent"
          :disabled="agentOptions.length === 0"
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
          :disabled="!selectedAgentId || agentOptions.length === 0"
          @click="confirmTransfer"
        />
      </div>
    </template>
  </UCard>
</template>
