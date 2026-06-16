<script setup lang="ts">
import { watch } from 'vue'

const route = useRoute()

watch(() => route.path, (newPath) => {
  if (newPath === '/settings' || newPath === '/settings/') {
    navigateTo('/settings/general')
  }
}, { immediate: true })
</script>

<template>
  <div class="space-y-6">
    <h1 class="flex items-center gap-3 text-2xl font-bold">
      <div class="flex items-center justify-center w-9 h-9 rounded-lg bg-yellow-400/10">
        <UIcon name="i-lucide-settings-2" class="w-5 h-5 text-yellow-400" />
      </div>
      Settings
    </h1>
    <NuxtPage />
  </div>
</template>
