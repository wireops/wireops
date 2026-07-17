<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
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

const toast = useToast()
const { getIntegrations, saveIntegration } = useIntegrations()
const integrationsList = ref<any[]>([])
const integrationsLoading = ref(false)

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
    docLink: 'https://ntfy.sh',
    open: integration => { ntfyIntegration.value = integration; showNtfyModal.value = true }
  },
  webhook: {
    icon: webhookIcon,
    description: 'Send event payloads to custom HTTP endpoints.',
    docLink: 'https://webhook.site',
    open: integration => { webhookIntegration.value = integration; showWebhookModal.value = true }
  },
  discord: {
    icon: discordIcon,
    description: 'Send sync notifications to a Discord channel.',
    docLink: 'https://support.discord.com/hc/en-us/articles/228383668-Intro-to-Webhooks',
    open: integration => { discordIntegration.value = integration; showDiscordModal.value = true }
  },
  slack: {
    icon: slackIcon,
    description: 'Send sync notifications to a Slack channel.',
    docLink: 'https://api.slack.com/messaging/webhooks',
    open: integration => { slackIntegration.value = integration; showSlackModal.value = true }
  },
  dozzle: {
    icon: dozzleIcon,
    description: 'Realtime log viewer for Docker containers.',
    docLink: 'https://dozzle.dev',
    open: integration => { dozzleIntegration.value = integration; showDozzleModal.value = true }
  },
  traefik: {
    icon: traefikIcon,
    description: 'HTTP reverse proxy and load balancer.',
    docLink: 'https://doc.traefik.io/traefik/',
    open: integration => { traefikIntegration.value = integration; showTraefikModal.value = true }
  },
  caddy: {
    icon: caddyIcon,
    description: 'Discover Caddy Docker Proxy routes from labels.',
    docLink: 'https://github.com/lucaslorentz/caddy-docker-proxy',
    open: integration => { caddyIntegration.value = integration; showCaddyModal.value = true }
  },
  'nginx-proxy-manager': {
    icon: nginxProxyManagerIcon,
    description: 'Open Nginx Proxy Manager routes from wireops labels.',
    docLink: 'https://nginxproxymanager.com/guide/',
    open: integration => { nginxProxyManagerIntegration.value = integration; showNginxProxyManagerModal.value = true }
  },
  vault: {
    icon: vaultIcon,
    description: 'Resolve secret env vars from a Vault KV v2 secrets engine.',
    docLink: 'https://developer.hashicorp.com/vault/docs',
    open: integration => { vaultIntegration.value = integration; showVaultModal.value = true }
  },
  infisical: {
    icon: infisicalIcon,
    description: 'Resolve secret env vars from Infisical via Universal Auth.',
    docLink: 'https://infisical.com/docs',
    open: integration => { infisicalIntegration.value = integration; showInfisicalModal.value = true }
  },
  sops: {
    icon: '',
    description: 'Decrypts secrets.yaml files committed next to wireops.yaml. Always active — each repository gets its own age key (see repository detail page).',
    docLink: 'https://github.com/getsops/sops',
    open: () => {}
  }
}

const configurableSlugs = Object.keys(integrationMeta).filter(slug => slug !== 'sops')


const groupedIntegrations = computed(() => {
  const groups: Record<string, any[]> = {}
  for (const item of integrationsList.value) {
    const cat = item.category || 'Other'
    if (!groups[cat]) groups[cat] = []
    groups[cat].push(item)
  }
  if (groups.Notification) {
    groups.Notification.sort((a, b) => String(a.name || a.slug).localeCompare(String(b.name || b.slug)))
  }
  return Object.keys(groups)
    .sort((a, b) => a.localeCompare(b))
    .reduce<Record<string, any[]>>((ordered, category) => {
      ordered[category] = groups[category]
      return ordered
    }, {})
})


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
  <div class="space-y-6">
    <div v-if="integrationsLoading" class="text-sm text-gray-500">Loading integrations...</div>
    <template v-else>
      <div v-for="(items, category) in groupedIntegrations" :key="category" class="mt-6 first:mt-0">
        <!-- Unified Section Card following settings general pattern -->
        <UCard class="shadow-none">
          <template #header>
            <div class="flex items-center justify-between w-full">
              <div class="flex items-center gap-3">
                <UBadge variant="subtle" color="primary" size="md" class="uppercase tracking-wider font-extrabold px-3 py-1">
                  {{ category }}
                </UBadge>
                <span class="text-xs text-gray-400 dark:text-wire-400/50 font-normal">({{ items.length }} integration{{ items.length > 1 ? 's' : '' }})</span>
              </div>
            </div>
          </template>
          
          <div class="pt-2">
            <div class="grid grid-cols-1 md:grid-cols-3 gap-6">
              <UCard 
                v-for="integration in items" 
                :key="integration.slug" 
                class="flex flex-col justify-between h-full bg-gray-50/20 dark:bg-carbon-900/10 border transition-all duration-300"
                :class="[
                  integration.enabled
                    ? 'border-primary-500 dark:border-primary-400 shadow-[0_0_12px_rgba(255,198,0,0.25)]'
                    : 'border-gray-150 dark:border-carbon-800/40 shadow-none'
                ]"
              >
                <template #header>
                  <div class="flex items-center justify-between">
                    <h3 class="font-bold text-base text-gray-950 dark:text-wire-200">{{ integration.name }}</h3>
                    <div class="flex items-center gap-2">
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
                      />
                    </div>
                  </div>
                </template>
                
                <div class="flex flex-col items-center justify-center p-6 space-y-4">
                  <!-- Large Icon -->
                  <div class="w-20 h-20 rounded-2xl bg-gray-50 dark:bg-carbon-800 flex items-center justify-center p-4 shadow-inner">
                    <img v-if="getIntegrationIcon(integration.slug)" :src="getIntegrationIcon(integration.slug)" class="w-12 h-12 object-contain" alt="">
                    <UIcon v-else :name="getIntegrationFallbackIcon(integration.slug)" class="w-10 h-10 text-primary-500" />
                  </div>
                  
                  <!-- Discrete Description -->
                  <p class="text-xs text-gray-500 dark:text-wire-200/60 text-center max-w-[220px] line-clamp-2">
                    {{ getIntegrationDescription(integration.slug) }}
                  </p>
                </div>
              </UCard>
            </div>
          </div>
        </UCard>
      </div>
    </template>

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
  </div>
</template>
