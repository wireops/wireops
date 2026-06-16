<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'

const toast = useToast()
const { getIntegrations, saveIntegration } = useIntegrations()
const integrationsList = ref<any[]>([])
const integrationsLoading = ref(false)

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
      toast.add({ title: 'Success', description: `${integration.slug} integration updated`, color: 'success' })
    } else {
      // Revert local state if save failed
      if (isToggle) {
        integration.enabled = !integration.enabled
      }
      toast.add({ title: 'Error', description: `Failed to update ${integration.slug}`, color: 'error' })
    }
  } catch (err: any) {
    // Revert local state on exception
    if (isToggle) {
      integration.enabled = !integration.enabled
    }
    toast.add({ title: 'Error', description: `An unexpected error occurred: ${err.message}`, color: 'error' })
  }
}

onMounted(() => {
  loadIntegrations()
})
</script>

<template>
  <div class="space-y-6">
    <div v-if="integrationsLoading" class="text-sm text-gray-500">Loading integrations...</div>
    <template v-else>
      <div v-for="(items, category) in groupedIntegrations" :key="category" class="space-y-4 mt-6 first:mt-0">
        <IntegrationCategory :category="String(category)" />
        
        <UCard v-for="integration in items" :key="integration.slug">
          <template #header>
            <div class="flex items-center justify-between">
              <div class="flex items-center gap-2">
                <img :src="`https://cdn.jsdelivr.net/gh/selfhst/icons/svg/${integration.slug}.svg`" class="w-5 h-5 object-contain" alt="">
                <h3 class="font-semibold">{{ integration.name }}</h3>
              </div>
              <USwitch v-model="integration.enabled" @update:model-value="handleSaveIntegration(integration, true)" />
            </div>
          </template>
          
          <div v-if="integration.enabled" class="space-y-4">
            <template v-if="integration.slug === 'dozzle'">
              <UFormField label="Dozzle URL" required>
                <UInput v-model="integration.config.url" placeholder="http://dozzle.local:8080" />
              </UFormField>
            </template>
            <template v-else-if="integration.slug === 'traefik'">
              <UFormField label="Scheme">
                <UInput v-model="integration.config.scheme" placeholder="https" />
                <p class="text-xs text-gray-500 mt-1">Default is https</p>
              </UFormField>
              <UFormField label="Port">
                <UInput v-model="integration.config.port" placeholder="443" />
                <p class="text-xs text-gray-500 mt-1">Optional port to append to the URL</p>
              </UFormField>
            </template>

            <template v-else>
              <p class="text-sm text-gray-500 italic">No additional configuration required.</p>
            </template>
            
            <div class="flex justify-end pt-2">
              <UButton label="Save Config" size="sm" @click="handleSaveIntegration(integration, false)" />
            </div>
          </div>
          <div v-else>
            <p class="text-sm text-gray-500 italic">Enable this integration to configure its details.</p>
          </div>
        </UCard>
      </div>
    </template>
  </div>
</template>
