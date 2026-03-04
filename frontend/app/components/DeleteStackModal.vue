<script setup lang="ts">
const { deleteStack } = useApi()
const toast = useToast()

const props = defineProps<{
  stack: any // the stack record (must have .id, .name, and optionally .expand.agent)
}>()

const emit = defineEmits<{
  deleted: []
  cancel: []
}>()

// Agent connectivity check
const agentOffline = computed(() => {
  const agent = props.stack?.expand?.agent
  if (!agent) return false // embedded / unknown, allow
  // An agent is considered offline if its last_seen is >2 minutes ago OR status is not ACTIVE
  if (agent.status && agent.status !== 'ACTIVE') return true
  return false
})

const agentName = computed(() => props.stack?.expand?.agent?.hostname || 'Agent')

const forceDelete = ref(false)
const deleting = ref(false)
const errorMsg = ref('')

async function confirmDelete() {
  if (agentOffline.value && !forceDelete.value) return
  deleting.value = true
  errorMsg.value = ''
  try {
    const res = await deleteStack(props.stack.id, forceDelete.value)
    if (res?.error) {
      errorMsg.value = res.error
      return
    }
    toast.add({ title: `Stack "${props.stack.name}" deleted`, color: 'success' })
    emit('deleted')
  } catch (e: any) {
    errorMsg.value = e?.message || 'Unexpected error'
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
      <!-- Agent offline warning -->
      <div v-if="agentOffline" class="flex items-start gap-3 rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3">
        <UIcon name="i-lucide-wifi-off" class="w-5 h-5 text-red-500 mt-0.5 shrink-0" />
        <div class="space-y-2">
          <div>
            <p class="text-sm font-medium text-red-500">Agent unavailable</p>
            <p class="text-xs text-red-500 mt-0.5">
              <span class="font-mono">{{ agentName }}</span> is offline. You can force delete the database records, but the containers on the host will become orphaned.
            </p>
          </div>
          <UCheckbox v-model="forceDelete" color="red" label="Force delete database records only" class="pt-2" />
        </div>
      </div>

      <!-- Confirmation text -->
      <div v-else class="text-sm text-gray-500 space-y-1">
        <p>Are you sure you want to delete <span class="font-semibold text-gray-800 dark:text-gray-200">{{ stack?.name }}</span>?</p>
        <p class="text-xs">The agent will run <code class="bg-gray-100 dark:bg-gray-800 px-1 rounded">docker compose down</code> to stop all containers before removing the stack.</p>
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
          label="Delete Stack"
          color="error"
          icon="i-lucide-trash-2"
          :loading="deleting"
          :disabled="agentOffline && !forceDelete"
          @click="confirmDelete"
        />
      </div>
    </template>
  </UCard>
</template>
