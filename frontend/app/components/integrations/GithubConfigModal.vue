<script setup lang="ts">
import { ref, watch } from 'vue'

const props = defineProps<{
  integration: any
}>()

const emit = defineEmits<{
  saved: []
}>()

const isOpen = defineModel<boolean>('open', { default: false })
const toast = useToast()
const { listGitProviders } = useApi()
const { testGithubIntegration } = useIntegrations()

const loading = ref(false)
const testing = ref(false)
const connected = ref(false)
const accountLogin = ref('')

async function refreshStatus() {
  loading.value = true
  try {
    const providers = await listGitProviders()
    const github = providers.find(p => p.slug === 'github')
    connected.value = !!github?.connected
    accountLogin.value = github?.account_login || ''
  } catch {
    connected.value = false
  } finally {
    loading.value = false
  }
}

watch(isOpen, (open) => {
  if (open) refreshStatus()
})

async function handleConnected() {
  await refreshStatus()
  emit('saved')
  toast.add({ title: 'GitHub connected', color: 'success' })
}

async function testConnection() {
  testing.value = true
  try {
    const result = await testGithubIntegration()
    if (result.success === 'true') {
      toast.add({ title: 'Connection successful', color: 'success' })
    } else {
      toast.add({ title: 'Connection failed', description: result.error, color: 'error' })
    }
  } catch (e: any) {
    toast.add({ title: 'Connection failed', description: e.message, color: 'error' })
  } finally {
    testing.value = false
  }
}

function close() {
  isOpen.value = false
}
</script>

<template>
  <UModal
    v-model:open="isOpen" :ui="{ content: 'bg-gray-50 dark:bg-(--ui-bg)' }"
    title="GitHub"
    description="Native GitHub OAuth connection — used to browse and pick repositories when adding one."
  >
    <template #body>
      <div class="space-y-4" role="document">
        <p class="text-xs text-gray-400">
          Enabled automatically when <code>GITHUB_OAUTH_CLIENT_ID</code> / <code>GITHUB_OAUTH_CLIENT_SECRET</code>
          are set on the server — it can't be toggled manually here.
        </p>

        <div v-if="loading" class="text-sm text-gray-500">Loading status...</div>

        <template v-else>
          <div v-if="connected" class="flex items-center gap-2">
            <UIcon name="i-lucide-check-circle-2" class="w-4 h-4 text-success-500" />
            <span class="text-sm">Connected as <strong>@{{ accountLogin }}</strong></span>
          </div>
          <div v-else class="space-y-3">
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-circle-dashed" class="w-4 h-4 text-gray-400" />
              <span class="text-sm text-gray-500">Not connected yet</span>
            </div>
            <ConnectGithubButton v-if="props.integration?.enabled" @connected="handleConnected" />
            <p v-else class="text-xs text-amber-600 dark:text-amber-400">
              Set the OAuth env vars above and restart the server to enable connecting.
            </p>
          </div>
        </template>
      </div>
    </template>

    <template #footer>
      <div class="flex w-full items-center gap-2">
        <CancelButton @click="close" />
        <UButton
          v-if="connected"
          label="Test Connection"
          icon="i-lucide-plug"
          variant="outline"
          color="neutral"
          class="ml-auto"
          :loading="testing"
          @click="testConnection"
        />
      </div>
    </template>
  </UModal>
</template>
