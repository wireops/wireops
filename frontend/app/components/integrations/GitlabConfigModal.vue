<script setup lang="ts">
import { ref, computed, watch } from 'vue'

const props = defineProps<{
  integration: any
}>()

const emit = defineEmits<{
  saved: []
}>()

const isOpen = defineModel<boolean>('open', { default: false })
const toast = useToast()
const { listGitProviders } = useApi()
const { testGitlabIntegration } = useIntegrations()
const { copy } = useCopy()

const loading = ref(false)
const testing = ref(false)
const connected = ref(false)
const accountLogin = ref('')

const { $pb } = useNuxtApp()
const callbackUrl = computed(() => `${$pb.baseURL}/api/custom/git-providers/gitlab/callback`)

async function refreshStatus() {
  loading.value = true
  try {
    const providers = await listGitProviders()
    const gitlab = providers.find(p => p.slug === 'gitlab')
    connected.value = !!gitlab?.connected
    accountLogin.value = gitlab?.account_login || ''
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
  toast.add({ title: 'GitLab connected', color: 'success' })
}

async function testConnection() {
  testing.value = true
  try {
    const result = await testGitlabIntegration()
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
    title="GitLab"
    description="Native GitLab OAuth connection — used to browse and pick repositories when adding one. Works with gitlab.com and self-hosted instances."
  >
    <template #body>
      <div class="space-y-4" role="document">
        <p class="text-xs text-gray-400">
          Enabled automatically when <code>GITLAB_OAUTH_CLIENT_ID</code> / <code>GITLAB_OAUTH_CLIENT_SECRET</code>
          are set on the server — it can't be toggled manually here. Point <code>GITLAB_BASE_URL</code> at a
          self-hosted instance instead of gitlab.com if needed.
        </p>

        <div v-if="loading" class="text-sm text-gray-500">Loading status...</div>

        <template v-else>
          <div v-if="connected" class="space-y-3">
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-check-circle-2" class="w-4 h-4 text-success-500" />
              <span class="text-sm">Connected as <strong>@{{ accountLogin }}</strong></span>
            </div>
            <p class="text-xs text-gray-400">
              If the connection breaks (e.g. token revoked in GitLab), reconnect below — it re-authenticates the same
              credential in place, so existing repositories, stacks and jobs keep working without changes.
            </p>
            <ConnectGitlabButton label="Reconnect GitLab" @connected="handleConnected" />
          </div>
          <div v-else class="space-y-3">
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-circle-dashed" class="w-4 h-4 text-gray-400" />
              <span class="text-sm text-gray-500">Not connected yet</span>
            </div>
            <ConnectGitlabButton v-if="props.integration?.enabled" @connected="handleConnected" />
            <div v-else class="space-y-3">
              <p class="text-xs text-amber-600 dark:text-amber-400">
                Set the OAuth env vars above and restart the server to enable connecting.
              </p>

              <div class="rounded-lg border border-gray-200 dark:border-carbon-700 p-3 space-y-3">
                <p class="text-xs font-semibold text-gray-700 dark:text-wire-200">How to set up</p>

                <ol class="text-xs text-gray-500 dark:text-wire-200/70 space-y-2 list-decimal list-inside">
                  <li>
                    In GitLab, go to
                    <strong>User Settings → Applications</strong>
                    (self-hosted: same path on your instance, e.g.
                    <code>https://gitlab.example.com/-/user_settings/applications</code>).
                  </li>
                  <li>Click <strong>Add new application</strong> and give it a name (e.g. "wireops").</li>
                  <li>
                    Paste the callback URL below into <strong>Redirect URI</strong>.
                  </li>
                  <li>
                    Under <strong>Scopes</strong>, check <code>read_api</code> and <code>read_repository</code>. Leave
                    "Confidential" checked.
                  </li>
                  <li>Save the application, then copy the generated <strong>Application ID</strong> and <strong>Secret</strong>.</li>
                  <li>
                    On the server, set <code>GITLAB_OAUTH_CLIENT_ID</code> and <code>GITLAB_OAUTH_CLIENT_SECRET</code>
                    to those values (and <code>GITLAB_BASE_URL</code> if self-hosted, e.g.
                    <code>https://gitlab.example.com</code>), then restart wireops.
                  </li>
                </ol>

                <div class="space-y-1">
                  <span class="text-xs font-semibold text-gray-700 dark:text-wire-200">Redirect / Callback URL</span>
                  <div class="flex items-center gap-2">
                    <code class="flex-1 min-w-0 truncate text-xs bg-gray-100 dark:bg-carbon-800 rounded px-2 py-1">{{ callbackUrl }}</code>
                    <UButton
                      icon="i-lucide-copy"
                      size="xs"
                      variant="ghost"
                      color="neutral"
                      aria-label="Copy callback URL"
                      @click="copy(callbackUrl, 'Callback URL')"
                    />
                  </div>
                </div>
              </div>
            </div>
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
