<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import dozzleIcon from '~/assets/img/icons/integrations/dozzle.svg'
import traefikIcon from '~/assets/img/icons/integrations/traefik.svg'
import webhookIcon from '~/assets/img/icons/integrations/webhook.svg'
import ntfyIcon from '~/assets/img/icons/integrations/ntfy.svg'

const toast = useToast()
const { getIntegrations, saveIntegration } = useIntegrations()
const integrationsList = ref<any[]>([])
const integrationsLoading = ref(false)

const showNtfyModal = ref(false)
const ntfyIntegration = ref<any>(null)

const showWebhookModal = ref(false)
const webhookIntegration = ref<any>(null)

const showDozzleModal = ref(false)
const dozzleIntegration = ref<any>(null)

const showTraefikModal = ref(false)
const traefikIntegration = ref<any>(null)


const groupedIntegrations = computed(() => {
  const groups: Record<string, any[]> = {}
  for (const item of integrationsList.value) {
    const cat = item.category || 'Other'
    if (!groups[cat]) groups[cat] = []
    groups[cat].push(item)
  }
  return groups
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
  if (integration.slug === 'ntfy') {
    ntfyIntegration.value = integration
    showNtfyModal.value = true
  } else if (integration.slug === 'webhook') {
    webhookIntegration.value = integration
    showWebhookModal.value = true
  } else if (integration.slug === 'dozzle') {
    dozzleIntegration.value = integration
    showDozzleModal.value = true
  } else if (integration.slug === 'traefik') {
    traefikIntegration.value = integration
    showTraefikModal.value = true
  }
}

function getIntegrationIcon(slug: string) {
  if (slug === 'dozzle') return dozzleIcon
  if (slug === 'traefik') return traefikIcon
  if (slug === 'webhook') return webhookIcon
  if (slug === 'ntfy') return ntfyIcon
  return ''
}

function getIntegrationDescription(slug: string) {
  if (slug === 'dozzle') return 'Realtime log viewer for Docker containers.'
  if (slug === 'traefik') return 'HTTP reverse proxy and load balancer.'
  if (slug === 'webhook') return 'Send event payloads to custom HTTP endpoints.'
  if (slug === 'ntfy') return 'Push notifications to ntfy.sh or self-hosted topics.'
  return ''
}

function getIntegrationDocLink(slug: string) {
  if (slug === 'dozzle') return 'https://dozzle.dev'
  if (slug === 'traefik') return 'https://doc.traefik.io/traefik/'
  if (slug === 'ntfy') return 'https://ntfy.sh'
  if (slug === 'webhook') return 'https://webhook.site'
  return ''
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
                      <USwitch v-model="integration.enabled" @update:model-value="handleSaveIntegration(integration, true)" />
                      <UButton 
                        v-if="integration.slug === 'webhook' || integration.slug === 'ntfy' || integration.slug === 'dozzle' || integration.slug === 'traefik'"
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
                    <img :src="getIntegrationIcon(integration.slug)" class="w-12 h-12 object-contain" alt="">
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
  </div>
</template>
