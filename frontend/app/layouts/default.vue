<script setup lang="ts">
const { isAuthenticated, logout } = useAuth()
const route = useRoute()
const colorMode = useColorMode()
const mobileMenuOpen = ref(false)
const { isShowingHelp, shortcuts } = useKeyboard()

const navItems = [
  { label: 'Dashboard', icon: 'i-lucide-layout-dashboard', to: '/' },
  { label: 'Stacks', icon: 'i-lucide-container', to: '/stacks' },
  { label: 'Jobs', icon: 'i-lucide-calendar-clock', to: '/jobs' },
  { label: 'Repositories', icon: 'i-lucide-git-branch', to: '/repositories' },
  { label: 'Agents', icon: 'i-lucide-network', to: '/agents' },
  { label: 'Settings', icon: 'i-lucide-settings', to: '/settings' },
  { label: 'About', icon: 'i-lucide-info', to: '/about' },
]

function isActive(to: string) {
  if (to === '/') return route.path === '/'
  return route.path.startsWith(to)
}

watch(() => route.fullPath, () => {
  mobileMenuOpen.value = false
})
</script>

<template>
  <div class="min-h-screen bg-white dark:bg-carbon-950">
    <header class="dark sticky top-0 z-50 border-b border-carbon-800 bg-carbon-900">
      <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        <div class="flex h-14 items-center justify-between">
          <div class="flex items-center gap-6">
            <NuxtLink to="/" class="flex items-center gap-2">
              <UIcon name="i-lucide-zap" class="w-6 h-6 text-yellow-400 drop-shadow-[0_0_6px_rgba(255,198,0,0.6)]" />
              <span class="font-black text-lg tracking-widest uppercase text-yellow-400 drop-shadow-[0_0_8px_rgba(255,198,0,0.4)]">
                wireops
              </span>
            </NuxtLink>
            <nav v-if="isAuthenticated" class="hidden sm:flex items-center gap-1">
              <UButton
                v-for="item in navItems"
                :key="item.to"
                :to="item.to"
                :icon="item.icon"
                :label="item.label"
                :variant="isActive(item.to) ? 'soft' : 'ghost'"
                :color="isActive(item.to) ? 'primary' : 'neutral'"
                size="sm"
              />
            </nav>
          </div>
          <div v-if="isAuthenticated" class="flex items-center gap-2">
            <UButton
              :icon="colorMode.value === 'dark' ? 'i-lucide-sun' : 'i-lucide-moon'"
              variant="ghost"
              color="neutral"
              size="sm"
              @click="colorMode.preference = colorMode.value === 'dark' ? 'light' : 'dark'"
              :title="colorMode.value === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'"
            />
            <UButton
              icon="i-lucide-log-out"
              variant="ghost"
              color="neutral"
              size="sm"
              class="hidden sm:inline-flex"
              @click="logout"
            />
            <UButton
              :icon="mobileMenuOpen ? 'i-lucide-x' : 'i-lucide-menu'"
              variant="ghost"
              color="neutral"
              size="sm"
              class="sm:hidden"
              @click="mobileMenuOpen = !mobileMenuOpen"
            />
          </div>
        </div>
      </div>
      <div v-if="isAuthenticated && mobileMenuOpen" class="dark sm:hidden border-t border-carbon-800 bg-carbon-900">
        <nav class="flex flex-col px-4 py-2 gap-1">
          <UButton
            v-for="item in navItems"
            :key="item.to"
            :to="item.to"
            :icon="item.icon"
            :label="item.label"
            :variant="isActive(item.to) ? 'soft' : 'ghost'"
            :color="isActive(item.to) ? 'primary' : 'neutral'"
            size="sm"
            class="justify-start"
          />
          <div class="border-t border-carbon-800 mt-1 pt-1">
            <UButton
              icon="i-lucide-log-out"
              label="Logout"
              variant="ghost"
              color="neutral"
              size="sm"
              class="justify-start w-full"
              @click="logout"
            />
          </div>
        </nav>
      </div>
    </header>

    <main class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
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
