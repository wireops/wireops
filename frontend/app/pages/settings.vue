<script setup lang="ts">
import { computed, watch } from 'vue'

const route = useRoute()

watch(() => route.path, (newPath) => {
  if (newPath === '/settings' || newPath === '/settings/') {
    navigateTo('/settings/general', { replace: true })
  }
}, { immediate: true })

const sections = [
  { to: '/settings/general', icon: 'i-lucide-settings-2', title: 'General', subtitle: 'Global timezone, SSO, and retention settings for the wireops instance.' },
  { to: '/settings/backups', icon: 'i-lucide-database-backup', title: 'Backups', subtitle: 'Create, restore, and schedule full database and data directory backups.' },
  { to: '/settings/security', icon: 'i-lucide-shield', title: 'Security', subtitle: 'Manage credentials, sessions, and worker access policy.' },
  { to: '/settings/audit', icon: 'i-lucide-clipboard-list', title: 'Audit', subtitle: 'Review audit events and configure retention.' },
  { to: '/settings/integrations', icon: 'i-lucide-puzzle', title: 'Integrations', subtitle: 'Connect wireops to proxies, log viewers, notification channels and secret backends.' },
  { to: '/settings/identity', icon: 'i-lucide-users', title: 'Identity', subtitle: 'Manage users and service accounts.' },
]

const activeSection = computed(() => {
  return sections.find(section => route.path.startsWith(section.to)) ?? sections[0]
})
</script>

<template>
  <div class="space-y-6">
    <h1 class="flex items-center gap-3 text-2xl font-bold">
      <div class="flex items-center justify-center w-9 h-9 rounded-lg bg-yellow-400/10">
        <UIcon :name="activeSection.icon" class="w-5 h-5 text-yellow-400" />
      </div>
      <span>
        <span class="block">{{ activeSection.title }}</span>
        <span class="block text-sm font-normal text-gray-500 dark:text-wire-200/60">{{ activeSection.subtitle }}</span>
      </span>
    </h1>
    <NuxtPage />
  </div>
</template>
