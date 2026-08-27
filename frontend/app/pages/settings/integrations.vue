<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import dozzleIcon from '~/assets/img/icons/integrations/dozzle.svg'
import traefikIcon from '~/assets/img/icons/integrations/traefik.svg'
import caddyIcon from '~/assets/img/icons/integrations/caddy.svg'
import nginxProxyManagerIcon from '~/assets/img/icons/integrations/nginx-proxy-manager.svg'
import webhookIcon from '~/assets/img/icons/integrations/webhook.svg'
import ntfyIcon from '~/assets/img/icons/integrations/ntfy.svg'
import discordIcon from '~/assets/img/icons/integrations/discord.svg'
import slackIcon from '~/assets/img/icons/integrations/slack.svg'
import vaultIcon from '~/assets/img/icons/integrations/hashicorp-vault.svg'
import infisicalIcon from '~/assets/img/icons/integrations/infisical.svg'
import githubIcon from '~/assets/img/icons/integrations/github.svg'

const toast = useToast()
const { getIntegrations, saveIntegration } = useIntegrations()
const integrationsList = ref<any[]>([])
const integrationsLoading = ref(false)

const searchQuery = ref('')
const selectedCategories = ref<string[]>([])
const statusFilter = ref<'all' | 'enabled' | 'disabled'>('all')
const showMobileFilters = ref(false)

const statusOptions = [
  { label: 'All', value: 'all' },
  { label: 'Enabled', value: 'enabled' },
  { label: 'Disabled', value: 'disabled' },
] as const

const showNtfyModal = ref(false)
const ntfyIntegration = ref<any>(null)

const showWebhookModal = ref(false)
const webhookIntegration = ref<any>(null)

const showDiscordModal = ref(false)
const discordIntegration = ref<any>(null)

const showSlackModal = ref(false)
const slackIntegration = ref<any>(null)

const showDozzleModal = ref(false)
const dozzleIntegration = ref<any>(null)

const showTraefikModal = ref(false)
const traefikIntegration = ref<any>(null)

const showCaddyModal = ref(false)
const caddyIntegration = ref<any>(null)

const showNginxProxyManagerModal = ref(false)
const nginxProxyManagerIntegration = ref<any>(null)

const showVaultModal = ref(false)
const vaultIntegration = ref<any>(null)

const showInfisicalModal = ref(false)
const infisicalIntegration = ref<any>(null)

const showS3Modal = ref(false)
const s3Integration = ref<any>(null)

const showGithubModal = ref(false)
const githubIntegration = ref<any>(null)

interface IntegrationMeta {
  icon: string
  description: string
  docLink: string
  open: (integration: any) => void
}

const integrationMeta: Record<string, IntegrationMeta> = {
  ntfy: {
    icon: ntfyIcon,
    description: 'Push notifications to ntfy.sh or self-hosted topics.',
    docLink: 'https://wireops.dev/docs/integrations/notifications/ntfy/',
    open: integration => { ntfyIntegration.value = integration; showNtfyModal.value = true }
  },
  webhook: {
    icon: webhookIcon,
    description: 'Send event payloads to custom HTTP endpoints.',
    docLink: 'https://wireops.dev/docs/integrations/notifications/webhook/',
    open: integration => { webhookIntegration.value = integration; showWebhookModal.value = true }
  },
  discord: {
    icon: discordIcon,
    description: 'Send sync notifications to a Discord channel.',
    docLink: 'https://wireops.dev/docs/integrations/notifications/discord/',
    open: integration => { discordIntegration.value = integration; showDiscordModal.value = true }
  },
  slack: {
    icon: slackIcon,
    description: 'Send sync notifications to a Slack channel.',
    docLink: 'https://wireops.dev/docs/integrations/notifications/slack/',
    open: integration => { slackIntegration.value = integration; showSlackModal.value = true }
  },
  dozzle: {
    icon: dozzleIcon,
    description: 'Realtime log viewer for Docker containers.',
    docLink: 'https://wireops.dev/docs/integrations/logging/dozzle/',
    open: integration => { dozzleIntegration.value = integration; showDozzleModal.value = true }
  },
  traefik: {
    icon: traefikIcon,
    description: 'HTTP reverse proxy and load balancer.',
    docLink: 'https://wireops.dev/docs/integrations/reverse-proxy/traefik/',
    open: integration => { traefikIntegration.value = integration; showTraefikModal.value = true }
  },
  caddy: {
    icon: caddyIcon,
    description: 'Discover Caddy Docker Proxy routes from labels.',
    docLink: 'https://wireops.dev/docs/integrations/reverse-proxy/caddy/',
    open: integration => { caddyIntegration.value = integration; showCaddyModal.value = true }
  },
  'nginx-proxy-manager': {
    icon: nginxProxyManagerIcon,
    description: 'Open Nginx Proxy Manager routes from wireops labels.',
    docLink: 'https://wireops.dev/docs/integrations/reverse-proxy/nginx-proxy-manager/',
    open: integration => { nginxProxyManagerIntegration.value = integration; showNginxProxyManagerModal.value = true }
  },
  vault: {
    icon: vaultIcon,
    description: 'Resolve secret env vars from a Vault KV v2 secrets engine.',
    docLink: 'https://wireops.dev/docs/integrations/secrets/vault/',
    open: integration => { vaultIntegration.value = integration; showVaultModal.value = true }
  },
  infisical: {
    icon: infisicalIcon,
    description: 'Resolve secret env vars from Infisical via Universal Auth.',
    docLink: 'https://wireops.dev/docs/integrations/secrets/infisical/',
    open: integration => { infisicalIntegration.value = integration; showInfisicalModal.value = true }
  },
  s3: {
    icon: '',
    description: 'Mirror backups to S3-compatible storage (AWS S3, R2, MinIO, B2, ...).',
    docLink: 'https://wireops.dev/docs/integrations/storage/s3/',
    open: integration => { s3Integration.value = integration; showS3Modal.value = true }
  },
  github: {
    icon: githubIcon,
    description: 'Native GitHub OAuth connection — browse and pick repositories when adding one.',
    docLink: 'https://wireops.dev/docs/integrations/source-control/github/',
    open: integration => { githubIntegration.value = integration; showGithubModal.value = true }
  },
  sops: {
    icon: '',
    description: 'Decrypts secrets.yaml files committed next to wireops.yaml. Always active — each repository gets its own age key (see repository detail page).',
    docLink: 'https://wireops.dev/docs/integrations/secrets/sops/',
    open: () => {}
  }
}

const configurableSlugs = Object.keys(integrationMeta).filter(slug => slug !== 'sops')

const categoriesWithCounts = computed(() => {
  const counts: Record<string, number> = {}
  for (const item of integrationsList.value) {
    const cat = item.category || 'Other'
    counts[cat] = (counts[cat] || 0) + 1
  }
  return Object.keys(counts)
    .sort((a, b) => a.localeCompare(b))
    .map(category => ({ category, count: counts[category] }))
})

const filteredIntegrations = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  return integrationsList.value
    .filter((item) => {
      const cat = item.category || 'Other'
      if (selectedCategories.value.length && !selectedCategories.value.includes(cat)) return false
      if (statusFilter.value === 'enabled' && !item.enabled) return false
      if (statusFilter.value === 'disabled' && item.enabled) return false
      if (!query) return true
      return String(item.name || item.slug).toLowerCase().includes(query)
        || String(item.slug).toLowerCase().includes(query)
    })
    .sort((a, b) => String(a.name || a.slug).localeCompare(String(b.name || b.slug)))
})

function toggleCategory(category: string) {
  const idx = selectedCategories.value.indexOf(category)
  if (idx === -1) {
    selectedCategories.value = [...selectedCategories.value, category]
  } else {
    selectedCategories.value = selectedCategories.value.filter(c => c !== category)
  }
}

function clearFilters() {
  searchQuery.value = ''
  selectedCategories.value = []
  statusFilter.value = 'all'
}

async function loadIntegrations() {
  integrationsLoading.value = true
  try {
    integrationsList.value = await getIntegrations()
  } catch (e: any) {
    toast.add({ title: 'Failed to load integrations', color: 'error' })
  } finally {
    integrationsLoading.value = false
  }
}

async function handleSaveIntegration(integration: any, isToggle = false) {
  try {
    const success = await saveIntegration(integration.slug, integration.enabled, integration.config)
    if (success) {
      toast.add({ title: 'Success', description: `${integration.name} integration updated`, color: 'success' })
    } else {
      // Revert local state if save failed
      if (isToggle) {
        integration.enabled = !integration.enabled
      }
      toast.add({ title: 'Error', description: `Failed to update ${integration.name}`, color: 'error' })
    }
  } catch (err: any) {
    // Revert local state on exception
    if (isToggle) {
      integration.enabled = !integration.enabled
    }
    toast.add({ title: 'Error', description: `An unexpected error occurred: ${err.message}`, color: 'error' })
  }
}

function configureIntegration(integration: any) {
  integrationMeta[integration.slug]?.open(integration)
}

function getIntegrationIcon(slug: string) {
  return integrationMeta[slug]?.icon ?? ''
}

// Fallback for any future integration without a bundled SVG asset yet.
function getIntegrationFallbackIcon(slug: string) {
  if (slug === 'sops') return 'i-lucide-file-lock-2'
  if (slug === 's3') return 'i-lucide-database-backup'
  return 'i-lucide-puzzle'
}

function getIntegrationDescription(slug: string) {
  return integrationMeta[slug]?.description ?? ''
}

function getIntegrationDocLink(slug: string) {
  return integrationMeta[slug]?.docLink ?? ''
}

onMounted(() => {
  loadIntegrations()
})
</script>

<template>
  <div class="space-y-4">
    <div v-if="integrationsLoading" class="text-sm text-gray-500">Loading integrations...</div>
    <template v-else>
      <div class="grid grid-cols-1 lg:grid-cols-[1fr_240px] gap-6 items-start">
        <!-- Card grid -->
        <AppPanelCard>
          <!-- Search + mobile filter trigger -->
          <div class="flex items-center gap-2 mb-4">
            <AppTextInput
              v-model="searchQuery"
              icon="i-lucide-search"
              placeholder="Search integrations..."
              aria-label="Search integrations"
              class="flex-1"
            />
            <AppButtonInput
              icon="i-lucide-filter"
              label="Filters"
              aria-label="Open integration filters"
              class="lg:hidden"
              @click="showMobileFilters = true"
            >
              <template #trailing>
                <UBadge v-if="selectedCategories.length + (statusFilter !== 'all' ? 1 : 0)" size="xs" color="primary" variant="solid">
                  {{ selectedCategories.length + (statusFilter !== 'all' ? 1 : 0) }}
                </UBadge>
              </template>
            </AppButtonInput>
          </div>

          <div v-if="!filteredIntegrations.length" class="text-sm text-gray-500 py-12 text-center">
            No integrations match your search.
          </div>
          <div v-else class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-4">
            <div
              v-for="integration in filteredIntegrations"
              :key="integration.slug"
              class="flex flex-col gap-2 rounded-lg border border-gray-300 dark:border-carbon-700 p-3 bg-gray-50 dark:bg-carbon-800/40"
            >
              <div class="flex items-start justify-between gap-2">
                <div class="flex items-center gap-3 min-w-0">
                  <!-- Icon -->
                  <div class="w-10 h-10 shrink-0 rounded-lg bg-gray-50 dark:bg-carbon-800 flex items-center justify-center p-2 shadow-inner">
                    <GithubIcon v-if="integration.slug === 'github'" icon-class="w-6 h-6" />
                    <img v-else-if="getIntegrationIcon(integration.slug)" :src="getIntegrationIcon(integration.slug)" class="w-6 h-6 object-contain" alt="">
                    <UIcon v-else :name="getIntegrationFallbackIcon(integration.slug)" class="w-5 h-5 text-primary-500" />
                  </div>

                  <!-- Name -->
                  <div class="min-w-0 flex items-center gap-1.5">
                    <h3 class="font-semibold text-sm text-gray-950 dark:text-wire-200 truncate">{{ integration.name }}</h3>
                    <span
                      class="inline-block w-1.5 h-1.5 rounded-full shrink-0"
                      :class="integration.enabled ? 'bg-primary-500' : 'bg-gray-300 dark:bg-carbon-700'"
                      :title="integration.enabled ? 'Enabled' : 'Disabled'"
                    />
                  </div>
                </div>

                <!-- Actions -->
                <div class="flex items-center gap-1 shrink-0">
                  <UTooltip v-if="integration.locked" text="Always active — nothing to toggle">
                    <USwitch :model-value="integration.enabled" disabled />
                  </UTooltip>
                  <USwitch v-else v-model="integration.enabled" @update:model-value="handleSaveIntegration(integration, true)" />
                  <UButton
                    v-if="configurableSlugs.includes(integration.slug)"
                    icon="i-lucide-settings"
                    size="xs"
                    variant="ghost"
                    color="neutral"
                    :aria-label="`Configure ${integration.name}`"
                    @click="configureIntegration(integration)"
                  />
                  <UButton
                    v-if="getIntegrationDocLink(integration.slug)"
                    icon="i-lucide-external-link"
                    size="xs"
                    variant="ghost"
                    color="neutral"
                    :disabled="integration.slug === 'webhook'"
                    :to="getIntegrationDocLink(integration.slug)"
                    target="_blank"
                    :aria-label="`${integration.name} documentation (opens in a new tab)`"
                  />
                </div>
              </div>

              <p class="text-xs text-gray-500 dark:text-wire-200/60 line-clamp-2 min-h-8">
                {{ getIntegrationDescription(integration.slug) }}
              </p>
            </div>
          </div>
        </AppPanelCard>

        <!-- Category filter panel (desktop) -->
        <div class="hidden lg:block sticky top-4">
          <AppPanelCard class="shadow-none">
            <template #header>
              <div class="flex items-center justify-between">
                <span class="text-xs font-bold uppercase tracking-wider text-gray-500 dark:text-wire-400/70">Filters</span>
                <UButton
                  v-if="selectedCategories.length || searchQuery || statusFilter !== 'all'"
                  size="xs"
                  variant="ghost"
                  color="neutral"
                  @click="clearFilters"
                >
                  Clear
                </UButton>
              </div>
            </template>
            <div class="space-y-4">
              <div class="space-y-2">
                <span class="text-xs font-semibold text-gray-500 dark:text-wire-400/70">Status</span>
                <div class="flex gap-1">
                  <UButton
                    v-for="option in statusOptions"
                    :key="option.value"
                    size="xs"
                    :variant="statusFilter === option.value ? 'solid' : 'outline'"
                    :color="statusFilter === option.value ? 'primary' : 'neutral'"
                    class="flex-1 justify-center"
                    @click="statusFilter = option.value"
                  >
                    {{ option.label }}
                  </UButton>
                </div>
              </div>
              <div class="space-y-2">
                <span class="text-xs font-semibold text-gray-500 dark:text-wire-400/70">Categories</span>
                <label
                  v-for="{ category, count } in categoriesWithCounts"
                  :key="category"
                  class="flex items-center justify-between gap-2 cursor-pointer text-sm"
                >
                  <span class="flex items-center gap-2">
                    <UCheckbox
                      :model-value="selectedCategories.includes(category)"
                      @update:model-value="toggleCategory(category)"
                    />
                    <span class="text-gray-700 dark:text-wire-200">{{ category }}</span>
                  </span>
                  <span class="text-xs text-gray-400 dark:text-wire-400/50">{{ count }}</span>
                </label>
              </div>
            </div>
          </AppPanelCard>
        </div>
      </div>
    </template>

    <!-- Category filter panel (mobile) -->
    <USlideover v-model:open="showMobileFilters" title="Filters" class="w-full sm:w-[360px] max-w-full">
      <template #content>
        <div class="p-4 space-y-4">
          <div class="flex items-center justify-between">
            <h3 class="font-semibold text-lg">Filters</h3>
            <UButton
              v-if="selectedCategories.length || searchQuery || statusFilter !== 'all'"
              size="xs"
              variant="ghost"
              color="neutral"
              @click="clearFilters"
            >
              Clear all
            </UButton>
          </div>
          <div class="space-y-2">
            <span class="text-xs font-semibold text-gray-500 dark:text-wire-400/70">Status</span>
            <div class="flex gap-1">
              <UButton
                v-for="option in statusOptions"
                :key="option.value"
                size="xs"
                :variant="statusFilter === option.value ? 'solid' : 'outline'"
                :color="statusFilter === option.value ? 'primary' : 'neutral'"
                class="flex-1 justify-center"
                @click="statusFilter = option.value"
              >
                {{ option.label }}
              </UButton>
            </div>
          </div>
          <div class="space-y-2">
            <span class="text-xs font-semibold text-gray-500 dark:text-wire-400/70">Categories</span>
            <label
              v-for="{ category, count } in categoriesWithCounts"
              :key="category"
              class="flex items-center justify-between gap-2 cursor-pointer text-sm"
            >
              <span class="flex items-center gap-2">
                <UCheckbox
                  :model-value="selectedCategories.includes(category)"
                  @update:model-value="toggleCategory(category)"
                />
                <span class="text-gray-700 dark:text-wire-200">{{ category }}</span>
              </span>
              <span class="text-xs text-gray-400 dark:text-wire-400/50">{{ count }}</span>
            </label>
          </div>
          <UButton block color="primary" @click="showMobileFilters = false">Show results</UButton>
        </div>
      </template>
    </USlideover>

    <IntegrationsNtfyConfigModal
      v-model:open="showNtfyModal"
      :integration="ntfyIntegration"
      @saved="loadIntegrations"
    />

    <IntegrationsWebhookConfigModal
      v-model:open="showWebhookModal"
      :integration="webhookIntegration"
      @saved="loadIntegrations"
    />

    <IntegrationsDiscordConfigModal
      v-model:open="showDiscordModal"
      :integration="discordIntegration"
      @saved="loadIntegrations"
    />

    <IntegrationsSlackConfigModal
      v-model:open="showSlackModal"
      :integration="slackIntegration"
      @saved="loadIntegrations"
    />

    <IntegrationsDozzleConfigModal
      v-model:open="showDozzleModal"
      :integration="dozzleIntegration"
      @saved="loadIntegrations"
    />

    <IntegrationsTraefikConfigModal
      v-model:open="showTraefikModal"
      :integration="traefikIntegration"
      @saved="loadIntegrations"
    />

    <IntegrationsCaddyConfigModal
      v-model:open="showCaddyModal"
      :integration="caddyIntegration"
      @saved="loadIntegrations"
    />

    <IntegrationsNginxProxyManagerConfigModal
      v-model:open="showNginxProxyManagerModal"
      :integration="nginxProxyManagerIntegration"
      @saved="loadIntegrations"
    />

    <IntegrationsVaultConfigModal
      v-model:open="showVaultModal"
      :integration="vaultIntegration"
      @saved="loadIntegrations"
    />

    <IntegrationsInfisicalConfigModal
      v-model:open="showInfisicalModal"
      :integration="infisicalIntegration"
      @saved="loadIntegrations"
    />

    <IntegrationsS3ConfigModal
      v-model:open="showS3Modal"
      :integration="s3Integration"
      @saved="loadIntegrations"
    />

    <IntegrationsGithubConfigModal
      v-model:open="showGithubModal"
      :integration="githubIntegration"
      @saved="loadIntegrations"
    />
  </div>
</template>
