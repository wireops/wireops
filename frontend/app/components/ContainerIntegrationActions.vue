<script setup lang="ts">
import { computed } from 'vue'
import type { IntegrationAction } from '../composables/useIntegrations'

const props = defineProps<{
  actions?: IntegrationAction[]
  containerId: string
  containerName: string
}>()

const emit = defineEmits<{
  (e: 'show-logs', containerId: string, containerName: string): void
}>()

const linkActions = computed(() => {
  return props.actions?.filter(a => a.kind === 'reverse-proxy') || []
})

const replaceLogActions = computed(() => {
  return props.actions?.filter(a => a.kind === 'log') || []
})

const hasReplacedLogs = computed(() => replaceLogActions.value.length > 0)

function openAction(action: IntegrationAction) {
  if (action.url) {
    window.open(action.url, '_blank')
  }
}
</script>

<template>
  <div class="flex items-center gap-1">
    <!-- Link integrations (e.g., Traefik external links) -->
    <UTooltip v-for="action in linkActions" :key="action.integration_slug" :text="action.label">
      <UButton
        variant="ghost"
        color="neutral"
        size="xs"
        @click="openAction(action)"
      >
        <template #leading>
          <img :src="`https://cdn.jsdelivr.net/gh/selfhst/icons/svg/${action.integration_slug}.svg`" class="w-4 h-4 object-contain" alt="">
        </template>
      </UButton>
    </UTooltip>

    <!-- Logs button or Replaced Logs button (e.g., Dozzle) -->
    <template v-if="hasReplacedLogs">
      <UTooltip v-for="action in replaceLogActions" :key="action.integration_slug" :text="action.label">
        <UButton
          variant="ghost"
          color="neutral"
          size="xs"
          @click="openAction(action)"
        >
          <template #leading>
            <img :src="`https://cdn.jsdelivr.net/gh/selfhst/icons/svg/${action.integration_slug}.svg`" class="w-4 h-4 object-contain" alt="">
          </template>
        </UButton>
      </UTooltip>
    </template>
    <template v-else>
      <UTooltip text="View Logs">
        <UButton
          icon="i-lucide-scroll-text"
          variant="ghost"
          color="neutral"
          size="xs"
          @click="emit('show-logs', containerId, containerName)"
        />
      </UTooltip>
    </template>
  </div>
</template>
