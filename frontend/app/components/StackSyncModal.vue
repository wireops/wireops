<script setup lang="ts">
const { triggerSync } = useApi()
const toast = useToast()
const { announce } = useA11yAnnouncer()

const props = defineProps<{
  stack: any
}>()

const emit = defineEmits<{
  synced: []
}>()

const isOpen = defineModel<boolean>('open', { default: false })
const syncing = ref(false)

function close() {
  isOpen.value = false
}

async function confirmSync() {
  if (!props.stack?.id) return

  syncing.value = true
  try {
    await triggerSync(props.stack.id)
    toast.add({ title: 'Sync triggered', color: 'success' })
    announce(`Manual sync started for ${props.stack.name}`)
    emit('synced')
    close()
  } catch (e: any) {
    toast.add({ title: e?.message || 'Sync failed', color: 'error' })
    announce(`Sync failed for ${props.stack?.name || 'stack'}`, 'assertive')
  } finally {
    syncing.value = false
  }
}
</script>

<template>
  <UModal
    v-model:open="isOpen"
    title="Confirm Sync"
    description="Confirm this manual sync before continuing."
  >
    <template #body>
      <div class="space-y-4" role="document">
        <div class="rounded-lg border border-primary/20 bg-primary/5 px-4 py-3">
          <p class="text-sm text-gray-700 dark:text-wire-200/80">
            This will sync the latest Git changes and update the Docker stack on the worker.
          </p>
        </div>

        <div v-if="stack" class="space-y-1 text-sm text-gray-600 dark:text-wire-200/60">
          <p>
            Stack:
            <code class="rounded bg-gray-100 px-1.5 py-0.5 text-xs font-medium text-gray-900 dark:bg-carbon-800 dark:text-wire-200">
              {{ stack.name }}
            </code>
          </p>
          <p>
            Repository:
            <code class="rounded bg-gray-100 px-1.5 py-0.5 text-xs font-medium text-gray-900 dark:bg-carbon-800 dark:text-wire-200">
              {{ stack.expand?.repository?.name || 'Unknown repo' }}
            </code>
          </p>
        </div>
      </div>
    </template>

    <template #footer>
      <div class="flex w-full items-center gap-2">
        <UButton label="Cancel" variant="outline" @click="close" />
        <UButton
          label="Run Sync"
          color="primary"
          icon="i-lucide-refresh-cw"
          class="ml-auto"
          :loading="syncing"
          @click="confirmSync"
        />
      </div>
    </template>
  </UModal>
</template>
