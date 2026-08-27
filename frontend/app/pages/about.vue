<script setup lang="ts">
const { getSystemInfo } = useApi()
const { startTour } = useOnboardingTour()

const { data: systemInfo, refresh: refreshSystemInfo } = useAsyncData('system_info', () => getSystemInfo())

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / Math.pow(k, i)).toFixed(2)} ${sizes[i]}`
}

function truncatePath(path: string): string {
  if (!path) return ''
  if (path.length > 40) {
    return path.slice(0, 15) + '...' + path.slice(-20)
  }
  return path
}
</script>

<template>
  <div class="max-w-3xl mx-auto space-y-8 py-8">

    <!-- Hero Section -->
    <div class="flex flex-col items-center text-center space-y-6">
      <div class="relative flex flex-col items-center justify-center gap-2 rounded-3xl border border-yellow-400/20 bg-carbon-900 px-10 py-8 overflow-hidden">
        <div class="absolute inset-0 bg-[radial-gradient(circle_at_50%_45%,rgba(255,202,21,0.18),transparent_66%)] pointer-events-none" />
        <img src="~/assets/img/logo.png" alt="" class="relative w-24 h-24 object-contain drop-shadow-[0_0_12px_rgba(255,198,0,0.5)]">
        <span class="relative text-2xl font-black tracking-tight bg-[linear-gradient(100deg,#facc15_4%,#d5cc83_48%,#8dc3e7_100%)] bg-clip-text text-transparent">wireops</span>
      </div>

      <div class="space-y-2">
        <p class="text-wire-400 tracking-wide">GitOps Controller for Docker Compose</p>
        <p class="text-gray-500 dark:text-wire-200/50 text-sm max-w-lg mx-auto">
          Self-hosted. Single binary. Embedded PocketBase.
        </p>
      </div>

      <div class="flex flex-wrap items-center justify-center gap-3">
        <UButton
          to="https://github.com/wireops/wireops"
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
          label="Donate"
          color="error"
          variant="solid"
          size="md"
        />
        <UButton
          to="https://ko-fi.com/jfxdev"
          target="_blank"
          icon="i-simple-icons-kofi"
          label="Ko-fi"
          aria-label="Ko-fi (opens in a new tab)"
          color="info"
          variant="solid"
          size="md"
        />
        <UButton
          to="https://github.com/wireops/wireops/blob/main/docs/DEVELOPMENT.md"
          target="_blank"
          icon="i-lucide-book"
          label="Docs"
          color="neutral"
          variant="outline"
          size="md"
        />
        <UButton
          icon="i-lucide-compass"
          label="Tour"
          color="warning"
          variant="outline"
          size="md"
          @click="startTour()"
        />
      </div>
    </div>

    <!-- System Info -->
    <AppPanelCard>
      <template #header>
        <div class="flex items-center justify-between">
          <h3 class="font-semibold text-gray-900 dark:text-wire-200">System Information</h3>
          <UButton icon="i-lucide-refresh-cw" variant="ghost" color="neutral" size="xs" title="Refresh" @click="refreshSystemInfo()" />
        </div>
      </template>
      <div v-if="systemInfo" class="grid grid-cols-1 sm:grid-cols-2 gap-3 text-sm">
        <div class="flex items-start gap-3 p-3 rounded-xl bg-gray-100 border border-gray-300 dark:bg-carbon-800/40 dark:border-carbon-700">
          <div class="p-2 rounded-lg bg-yellow-400/10 border border-yellow-400/10">
            <UIcon name="i-lucide-zap" class="w-5 h-5 text-yellow-400" />
          </div>
          <div>
            <p class="text-xs text-gray-500 dark:text-wire-200/40 uppercase tracking-wider font-semibold">wireops Version</p>
            <p class="text-lg font-bold text-gray-900 dark:text-wire-200">{{ systemInfo.version }}</p>
          </div>
        </div>

        <div class="flex items-start gap-3 p-3 rounded-xl bg-gray-100 border border-gray-300 dark:bg-carbon-800/40 dark:border-carbon-700">
          <div class="p-2 rounded-lg bg-wire-700/20 border border-wire-700/20">
            <UIcon name="i-lucide-hard-drive" class="w-5 h-5 text-wire-400" />
          </div>
          <div class="flex-1 min-w-0">
            <p class="text-xs text-gray-500 dark:text-wire-200/40 uppercase tracking-wider font-semibold">Workspace Storage</p>
            <p class="text-lg font-bold text-gray-900 dark:text-wire-200">{{ formatBytes(systemInfo.disk_usage) }}</p>
            <p class="text-xs text-gray-400 dark:text-wire-200/30 mt-1 font-mono truncate" :title="systemInfo.workspace_path">{{ truncatePath(systemInfo.workspace_path) }}</p>
          </div>
        </div>
      </div>
      <div v-else class="flex items-center justify-center py-8 text-wire-400">
        <UIcon name="i-lucide-loader-2" class="w-6 h-6 animate-spin" />
      </div>
    </AppPanelCard>

    <div class="text-center text-sm text-gray-400 dark:text-wire-200/30">
      <p>Made with ❤️ by <a href="https://github.com/jfxdev" target="_blank" rel="noopener noreferrer" aria-label="jfxdev (opens in a new tab)" class="hover:text-yellow-400 transition-colors">jfxdev</a></p>
    </div>
  </div>
</template>
