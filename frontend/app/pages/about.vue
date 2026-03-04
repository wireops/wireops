<script setup lang="ts">
const { getSystemInfo } = useApi()

const { data: systemInfo, refresh: refreshSystemInfo } = useAsyncData('system_info', () => getSystemInfo())

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / Math.pow(k, i)).toFixed(2)} ${sizes[i]}`
}
</script>

<template>
  <div class="max-w-3xl mx-auto space-y-8 py-8">

    <!-- Hero Section -->
    <div class="flex flex-col items-center text-center space-y-6">
      <div class="relative">
        <div class="w-24 h-24 rounded-2xl bg-yellow-400/10 border border-yellow-400/20 flex items-center justify-center shadow-[0_0_40px_rgba(255,198,0,0.12)]">
          <UIcon name="i-lucide-zap" class="w-14 h-14 text-yellow-400 drop-shadow-[0_0_12px_rgba(255,198,0,0.7)]" />
        </div>
        <div class="absolute -inset-4 rounded-3xl bg-yellow-400/5 blur-xl pointer-events-none" />
      </div>

      <div class="space-y-2">
        <h1 class="text-3xl font-black tracking-widest uppercase text-yellow-400 drop-shadow-[0_0_12px_rgba(255,198,0,0.4)]">
          wireops
        </h1>
        <p class="text-wire-400 tracking-wide">GitOps Controller for Docker Compose</p>
        <p class="text-wire-200/50 text-sm max-w-lg mx-auto">
          Self-hosted. Single binary. Embedded PocketBase.
        </p>
      </div>

      <div class="flex flex-wrap items-center justify-center gap-3">
        <UButton
          to="https://github.com/jfxdev/wireops"
          target="_blank"
          icon="i-lucide-github"
          label="GitHub"
          color="neutral"
          variant="solid"
          size="md"
        />
        <UButton
          to="https://github.com/sponsors/jfxdev"
          target="_blank"
          icon="i-lucide-heart"
          label="Sponsor"
          color="error"
          variant="solid"
          size="md"
        />
        <UButton
          to="https://www.buymeacoffee.com/jfxdev"
          target="_blank"
          icon="i-lucide-coffee"
          label="Buy me a coffee"
          color="primary"
          variant="solid"
          size="md"
        />
        <UButton
          to="https://github.com/jfxdev/wireops/blob/main/docs/DEVELOPMENT.md"
          target="_blank"
          icon="i-lucide-book"
          label="Docs"
          color="neutral"
          variant="outline"
          size="md"
        />
      </div>
    </div>

    <!-- System Info -->
    <UCard>
      <template #header>
        <div class="flex items-center justify-between">
          <h3 class="font-semibold text-wire-200">System Information</h3>
          <UButton icon="i-lucide-refresh-cw" variant="ghost" color="neutral" size="xs" @click="refreshSystemInfo()" title="Refresh" />
        </div>
      </template>
      <div v-if="systemInfo" class="grid grid-cols-1 sm:grid-cols-2 gap-3 text-sm">
        <div class="flex items-start gap-3 p-3 rounded-xl bg-carbon-800/40 border border-carbon-700">
          <div class="p-2 rounded-lg bg-yellow-400/10 border border-yellow-400/10">
            <UIcon name="i-lucide-zap" class="w-5 h-5 text-yellow-400" />
          </div>
          <div>
            <p class="text-xs text-wire-200/40 uppercase tracking-wider font-semibold">wireops Version</p>
            <p class="text-lg font-bold text-wire-200">{{ systemInfo.version }}</p>
          </div>
        </div>

        <div class="flex items-start gap-3 p-3 rounded-xl bg-carbon-800/40 border border-carbon-700">
          <div class="p-2 rounded-lg bg-wire-400/10 border border-wire-400/10">
            <UIcon name="i-lucide-container" class="w-5 h-5 text-wire-400" />
          </div>
          <div>
            <p class="text-xs text-wire-200/40 uppercase tracking-wider font-semibold">Docker</p>
            <p class="text-lg font-bold text-wire-200">{{ systemInfo.docker_version }}</p>
          </div>
        </div>

        <div class="flex items-start gap-3 p-3 rounded-xl bg-carbon-800/40 border border-carbon-700">
          <div class="p-2 rounded-lg bg-wire-700/20 border border-wire-700/20">
            <UIcon name="i-lucide-layers" class="w-5 h-5 text-wire-400" />
          </div>
          <div>
            <p class="text-xs text-wire-200/40 uppercase tracking-wider font-semibold">Docker Compose</p>
            <p class="text-lg font-bold text-wire-200">{{ systemInfo.compose_version }}</p>
          </div>
        </div>

        <div class="flex items-start gap-3 p-3 rounded-xl bg-carbon-800/40 border border-carbon-700">
          <div class="p-2 rounded-lg bg-yellow-400/10 border border-yellow-400/10">
            <UIcon name="i-lucide-database" class="w-5 h-5 text-yellow-400/80" />
          </div>
          <div>
            <p class="text-xs text-wire-200/40 uppercase tracking-wider font-semibold">Resources</p>
            <p class="text-sm font-semibold text-wire-200">{{ systemInfo.repositories }} repos · {{ systemInfo.stacks }} stacks</p>
          </div>
        </div>

        <div class="flex items-start gap-3 p-3 rounded-xl bg-carbon-800/40 border border-carbon-700 sm:col-span-2">
          <div class="p-2 rounded-lg bg-wire-700/20 border border-wire-700/20">
            <UIcon name="i-lucide-hard-drive" class="w-5 h-5 text-wire-400" />
          </div>
          <div class="flex-1">
            <p class="text-xs text-wire-200/40 uppercase tracking-wider font-semibold">Workspace Storage</p>
            <p class="text-lg font-bold text-wire-200">{{ formatBytes(systemInfo.disk_usage) }}</p>
            <p class="text-xs text-wire-200/30 mt-1 font-mono">{{ systemInfo.workspace_path }}</p>
          </div>
        </div>
      </div>
      <div v-else class="flex items-center justify-center py-8 text-wire-400">
        <UIcon name="i-lucide-loader-2" class="w-6 h-6 animate-spin" />
      </div>
    </UCard>

    <div class="text-center text-sm text-wire-200/30">
      <p>Made with ❤️ by jfxdev</p>
    </div>
  </div>
</template>
