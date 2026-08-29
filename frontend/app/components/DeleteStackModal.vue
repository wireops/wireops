<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import TerminalOutput from '~/components/TerminalOutput.vue'
import { useDeployStream } from '~/composables/useDeployStream'

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

// Snapshot the stack prop so the modal keeps showing the right name/logs even
// after deletion, once background refetches null it out on the parent page.
const stackSnapshot = ref(props.stack)
watch(() => props.stack, (val) => {
  if (val) stackSnapshot.value = val
})

// Worker connectivity check
const workerOffline = computed(() => {
  if (props.workerOffline) return true
  if (showForceDeleteOption.value) return true
  const worker = stackSnapshot.value?.expand?.worker
  if (!worker) return true
  // A worker is considered offline if its status is not ACTIVE
  if (worker.status && worker.status !== WORKER_STATUS.ACTIVE) return true
  return false
})

// Either reason (worker offline, or worker online but the compose file it
// needs is missing) requires the same force=true bypass and checkbox gate.
const requiresForce = computed(() => workerOffline.value || composeFileMissing.value)

const workerName = computed(() => stackSnapshot.value?.expand?.worker?.hostname || 'Assigned worker')

// Distinct from workerOffline: the worker's websocket connection is fine
// (it received the command and tried to run it) but Docker itself was
// unreachable on that host as of the last heartbeat — e.g. Docker Desktop
// restarting, a crashed daemon. Surfaced separately so the confirmation
// text doesn't conflate "can't reach the worker" with "worker is reachable
// but can't reach Docker".
const dockerOffline = computed(() =>
  !workerOffline.value && stackSnapshot.value?.expand?.worker?.docker_online === false
)

const forceDelete = ref(false)
const deleting = ref(false)
const deleted = ref(false)
const errorMsg = ref('')

// Distinct from an actually-offline worker: the worker is reachable but the
// backend couldn't find the rendered compose file it needs for
// `docker compose down` (e.g. DATA_DIR wiped/not persisted since the last
// render). Same force=true bypass on the backend, different reason — the
// "worker unavailable" copy would be misleading here.
const composeFileMissing = ref(false)

// Only opens the live SSE stream once the teardown dispatch is actually in
// flight — the delete endpoint runs `docker compose down` synchronously
// before removing DB records, and the worker streams that output under the
// same "teardown" phase tag used elsewhere (see useDeployStream).
const streamStackId = computed(() => (deleting.value || deleted.value ? stackSnapshot.value?.id : null))
const { lines: teardownLines } = useDeployStream(streamStackId)

function detectForceableError(message: string) {
  const lower = message.toLowerCase()
  if (lower.includes('offline')) {
    showForceDeleteOption.value = true
  }
  if (lower.includes('rendered compose file')) {
    composeFileMissing.value = true
  }
}

async function confirmDelete() {
  if (requiresForce.value && !forceDelete.value) return
  deleting.value = true
  errorMsg.value = ''
  try {
    const res = await deleteStack(stackSnapshot.value.id, forceDelete.value)
    if (res?.error) {
      errorMsg.value = res.error
      detectForceableError(res.error)
      return
    }
    toast.add({ title: `Stack "${stackSnapshot.value.name}" deleted`, color: 'success' })
    announce(`Stack ${stackSnapshot.value.name} deleted`)
    deleted.value = true
  } catch (e: any) {
    errorMsg.value = e?.message || 'Unexpected error'
    const composeOutput = e?.data?.compose_output?.trim()
    if (composeOutput && !errorMsg.value.includes(composeOutput)) {
      errorMsg.value += `\n${composeOutput}`
    }
    detectForceableError(errorMsg.value)
    announce(`Failed to delete stack ${stackSnapshot.value?.name || ''}`.trim(), 'assertive')
  } finally {
    deleting.value = false
  }
}

// Guarantees the parent navigates away even if the user dismisses the modal
// via backdrop/ESC after a successful delete instead of clicking "Close" —
// the parent's v-if unmounts this component on any close path. Tracked
// separately from `deleted` so an explicit "Close" click doesn't also fire
// this on the teardown it triggers, double-emitting 'deleted' to the parent.
let deletedEmitted = false
function closeAfterDelete() {
  deletedEmitted = true
  emit('deleted')
}
onUnmounted(() => {
  if (deleted.value && !deletedEmitted) emit('deleted')
})
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

      <!-- Docker unreachable on an otherwise-connected worker -->
      <div v-if="dockerOffline" class="flex items-start gap-3 rounded-lg border border-amber-500/30 bg-amber-500/10 px-4 py-3">
        <UIcon name="i-lucide-alert-triangle" class="w-5 h-5 text-amber-500 mt-0.5 shrink-0" />
        <p class="text-xs text-amber-600 dark:text-amber-400 leading-relaxed">
          <WorkerNameLabel :name="workerName" /> is connected, but Docker isn't responding on that host as of its
          last check-in. <code class="bg-gray-100 dark:bg-gray-800 px-1 rounded">docker compose down</code> will
          likely fail until the daemon is back up.
        </p>
      </div>

      <!-- Worker online, but the rendered compose file it needs is missing on disk -->
      <div v-if="composeFileMissing" class="flex items-start gap-3 rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3">
        <UIcon name="i-lucide-file-x" class="w-5 h-5 text-red-500 mt-0.5 shrink-0" />
        <div class="space-y-2">
          <div>
            <p class="text-sm font-medium text-red-500">Rendered compose file missing</p>
            <p class="text-xs text-red-500 mt-1 leading-relaxed">
              <WorkerNameLabel :name="workerName" /> is online, but the stack's rendered compose file is missing from storage, so
              <code class="bg-gray-100 dark:bg-gray-800 px-1 rounded">docker compose down</code> can't run. You can force delete the
              database records, but the containers on the host will become orphaned.
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
      <div v-if="!deleted && !requiresForce" class="text-sm text-gray-500 space-y-1">
        <p>Are you sure you want to delete <span class="font-semibold text-gray-800 dark:text-gray-200">{{ stackSnapshot?.name }}</span>?</p>
        <p class="text-xs">The worker will run <code class="bg-gray-100 dark:bg-gray-800 px-1 rounded">docker compose down</code> to stop all containers before removing the stack.</p>
        <p class="text-xs text-red-500 font-medium">This action cannot be undone.</p>
      </div>

      <!-- Success message -->
      <div v-if="deleted" class="flex items-start gap-3 rounded-lg border border-green-500/30 bg-green-500/10 px-4 py-3" role="status" aria-live="polite">
        <UIcon name="i-lucide-circle-check" class="w-5 h-5 text-green-500 mt-0.5 shrink-0" />
        <p class="text-sm text-green-600 dark:text-green-400">Stack "{{ stackSnapshot?.name }}" deleted successfully.</p>
      </div>

      <!-- API error -->
      <div v-if="errorMsg" class="flex items-start gap-3 rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3" role="alert" aria-live="assertive">
        <UIcon name="i-lucide-circle-x" class="w-5 h-5 text-red-500 mt-0.5 shrink-0" />
        <p class="text-sm text-red-500 whitespace-pre-line">{{ errorMsg }}</p>
      </div>

      <!-- Live teardown output while deletion is in flight / after completion -->
      <TerminalOutput v-if="(deleting || deleted) && teardownLines.length" :lines="teardownLines" />
    </div>

    <template #footer>
      <div class="flex justify-end gap-2">
        <CancelButton v-if="deleted" label="Close" @click="closeAfterDelete" />
        <template v-else>
          <CancelButton @click="emit('cancel')" />
          <UButton
            label="Delete Stack"
            color="error"
            icon="i-lucide-trash-2"
            :loading="deleting"
            :disabled="requiresForce && !forceDelete"
            @click="confirmDelete"
          />
        </template>
      </div>
    </template>
  </UCard>
</template>
