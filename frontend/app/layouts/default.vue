<script setup lang="ts">
import AppSidebar from './sidebar/AppSidebar.vue'

const { isAuthenticated, logout } = useAuth()
const route = useRoute()
const colorMode = useColorMode()
const mobileMenuOpen = ref(false)
const { isShowingHelp, shortcuts } = useKeyboard()

const navItems = [
  { label: 'Dashboard', icon: 'i-lucide-layout-dashboard', to: '/' },
  {
    label: 'Workloads',
    icon: 'i-lucide-container',
    to: '/workloads',
    children: [
      { label: 'Stacks', icon: 'i-lucide-layers', to: '/stacks' },
      { label: 'Jobs', icon: 'i-lucide-calendar-clock', to: '/jobs' },
    ],
  },
  { label: 'Repositories', icon: 'i-lucide-git-branch', to: '/repositories' },
  { label: 'Workers', icon: 'i-lucide-network', to: '/workers' },
  { label: 'Settings', icon: 'i-lucide-settings', to: '/settings' },
  { label: 'About', icon: 'i-lucide-info', to: '/about' },
]

function isActive(to: string) {
  if (to === '/') return route.path === '/'
  if (to === '/workloads') {
    return route.path.startsWith('/workloads') || route.path.startsWith('/stacks') || route.path.startsWith('/jobs')
  }
  return route.path.startsWith(to)
}

const activeNavLabel = computed(() => navItems.find(item => isActive(item.to))?.label || 'Menu')

function toggleTheme() {
  colorMode.preference = colorMode.value === 'dark' ? 'light' : 'dark'
}

function openHelp() {
  mobileMenuOpen.value = false
  isShowingHelp.value = true
}

function handleLogout() {
  mobileMenuOpen.value = false
  logout()
}

watch(() => route.fullPath, () => {
  mobileMenuOpen.value = false
})
</script>

<template>
  <div class="min-h-screen bg-white dark:bg-carbon-950">
    <div v-if="isAuthenticated" class="flex min-h-screen">
      <AppSidebar
        :nav-items="navItems"
        :current-path="route.path"
        :color-mode-value="colorMode.value"
        @help="openHelp"
        @toggle-theme="toggleTheme"
        @logout="handleLogout"
      />

      <div class="flex min-w-0 flex-1 flex-col">
        <div class="sticky top-0 z-40 border-b border-gray-200 bg-white/95 backdrop-blur lg:hidden dark:border-carbon-800 dark:bg-carbon-950/95">
          <div class="flex items-center justify-between px-4 py-3 sm:px-6">
            <div class="flex items-center gap-3">
              <UButton
                :icon="mobileMenuOpen ? 'i-lucide-x' : 'i-lucide-menu'"
                variant="outline"
                color="neutral"
                size="sm"
                @click="mobileMenuOpen = !mobileMenuOpen"
              />
              <div>
                <p class="text-xs uppercase tracking-[0.24em] text-gray-500 dark:text-wire-200/45">Navigation</p>
                <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ activeNavLabel }}</p>
              </div>
            </div>
            <NuxtLink to="/" class="flex items-center gap-2">
              <UIcon name="i-lucide-zap" class="h-5 w-5 text-yellow-400" />
              <span class="font-black text-sm tracking-[0.28em] uppercase text-yellow-400">wireops</span>
            </NuxtLink>
          </div>
        </div>

        <main class="flex-1">
          <div class="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
            <slot />
          </div>
        </main>
      </div>

      <AppSidebar
        mobile
        :open="mobileMenuOpen"
        :nav-items="navItems"
        :current-path="route.path"
        :color-mode-value="colorMode.value"
        @close="mobileMenuOpen = false"
        @help="openHelp"
        @toggle-theme="toggleTheme"
        @logout="handleLogout"
      />
    </div>

    <main v-else class="max-w-7xl mx-auto px-4 py-6 sm:px-6 lg:px-8">
      <slot />
    </main>

    <UModal v-model:open="isShowingHelp">
      <template #content>
        <UCard>
          <template #header>
            <div class="flex items-center justify-between">
              <div class="flex items-center gap-2">
                <UIcon name="i-lucide-keyboard" class="w-5 h-5 text-yellow-400" />
                <h2 class="font-semibold">Keyboard Shortcuts</h2>
              </div>
              <UButton icon="i-lucide-x" variant="ghost" color="neutral" size="xs" @click="isShowingHelp = false" />
            </div>
          </template>
          <div class="space-y-2">
            <div
              v-for="shortcut in shortcuts"
              :key="shortcut.key"
              class="flex items-center justify-between py-2 border-b border-gray-100 dark:border-carbon-800 last:border-0"
            >
              <span class="text-sm text-gray-600 dark:text-wire-200/70">{{ shortcut.description }}</span>
              <kbd class="px-2 py-1 text-xs font-semibold bg-gray-100 dark:bg-carbon-800 border border-gray-300 dark:border-carbon-700 rounded text-gray-700 dark:text-wire-200">
                {{ shortcut.key }}
              </kbd>
            </div>
          </div>
        </UCard>
      </template>
    </UModal>
  </div>
</template>
