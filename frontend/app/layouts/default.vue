<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import AppSidebar from './sidebar/AppSidebar.vue'

const { isAuthenticated, logout } = useAuth()
const route = useRoute()
const colorMode = useColorMode()
const mobileMenuOpen = ref(false)
const sidebarCollapsed = useCookie<boolean>('sidebar_collapsed', { default: () => false })
const { isShowingHelp, shortcuts } = useKeyboard()
const { announce } = useA11yAnnouncer()
const isShowingAccessibility = ref(false)

function toggleSidebar() {
  sidebarCollapsed.value = !sidebarCollapsed.value
}

const { isViewer } = usePermissions()

const navItems = computed(() => {
  return [
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
    ...(isViewer.value ? [] : [{ label: 'Workers', icon: 'i-lucide-network', to: '/workers' }]),
    ...(isViewer.value ? [] : [{
      label: 'Settings',
      icon: 'i-lucide-settings',
      to: '/settings',
      children: [
        { label: 'General', icon: 'i-lucide-settings-2', to: '/settings/general' },
        { label: 'Security', icon: 'i-lucide-shield', to: '/settings/security' },
        { label: 'Integrations', icon: 'i-lucide-puzzle', to: '/settings/integrations' },
        { label: 'Users', icon: 'i-lucide-users', to: '/settings/users' },
      ]
    }]),
    { label: 'About', icon: 'i-lucide-info', to: '/about' },
  ]
})

function isActive(to: string) {
  if (to === '/') return route.path === '/'
  if (to === '/workloads') {
    return route.path.startsWith('/workloads') || route.path.startsWith('/stacks') || route.path.startsWith('/jobs')
  }
  return route.path.startsWith(to)
}

const activeNavLabel = computed(() => {
  for (const item of navItems.value) {
    if (item.children?.some(child => isActive(child.to))) {
      return item.children.find(child => isActive(child.to))?.label || item.label
    }
    if (isActive(item.to)) return item.label
  }
  return 'Menu'
})

function toggleTheme() {
  colorMode.preference = colorMode.value === 'dark' ? 'light' : 'dark'
}

function openHelp() {
  mobileMenuOpen.value = false
  isShowingHelp.value = true
}

function openAccessibility() {
  mobileMenuOpen.value = false
  isShowingAccessibility.value = true
}

function handleLogout() {
  mobileMenuOpen.value = false
  logout()
}

function formatShortcutKey(key: string) {
  return key
    .replaceAll('Cmd/Ctrl', '__MAC_CMD_CTRL__')
    .replaceAll('Cmd', '__MAC_CMD__')
    .split(/(__MAC_CMD_CTRL__|__MAC_CMD__)/)
    .filter(Boolean)
}

watch(() => route.fullPath, () => {
  mobileMenuOpen.value = false
})

watch(mobileMenuOpen, (isOpen) => {
  announce(isOpen ? 'Navigation menu opened' : 'Navigation menu closed')
})
</script>

<template>
  <div class="min-h-screen bg-white dark:bg-carbon-950">
    <div :class="isAuthenticated ? 'flex min-h-screen' : 'min-h-screen'">
      <template v-if="isAuthenticated">
        <AppSidebar
          :nav-items="navItems"
          :current-path="route.path"
          :color-mode-value="colorMode.value"
          :collapsed="sidebarCollapsed"
          @toggle-collapse="toggleSidebar"
          @help="openHelp"
          @accessibility="openAccessibility"
          @toggle-theme="toggleTheme"
          @logout="handleLogout"
        />
      </template>

      <div :class="isAuthenticated ? 'flex min-w-0 flex-1 flex-col' : 'w-full'">
        <header
          v-if="isAuthenticated"
          class="sticky top-0 z-40 border-b border-gray-200 bg-white/95 backdrop-blur lg:hidden dark:border-carbon-800 dark:bg-carbon-950/95"
        >
          <div class="flex items-center justify-between px-4 py-3 sm:px-6">
            <div class="flex items-center gap-3">
              <UButton
                :icon="mobileMenuOpen ? 'i-lucide-x' : 'i-lucide-menu'"
                variant="outline"
                color="neutral"
                size="sm"
                :aria-label="mobileMenuOpen ? 'Close navigation menu' : 'Open navigation menu'"
                :aria-expanded="mobileMenuOpen"
                aria-controls="mobile-navigation"
                @click="mobileMenuOpen = !mobileMenuOpen"
              />
              <div>
                <p class="text-xs uppercase tracking-[0.24em] text-gray-500 dark:text-wire-200/45">Current section</p>
                <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ activeNavLabel }}</p>
              </div>
            </div>
            <NuxtLink to="/" class="flex items-center gap-2" aria-label="Go to dashboard">
              <UIcon name="i-lucide-zap" class="h-5 w-5 text-yellow-400" />
              <span class="font-black text-sm tracking-[0.28em] uppercase text-yellow-400">wireops</span>
            </NuxtLink>
          </div>
        </header>

        <main
          id="main-content"
          tabindex="-1"
          :class="isAuthenticated ? 'flex-1' : 'mx-auto min-h-screen max-w-7xl px-4 py-6 sm:px-6 lg:px-8'"
        >
          <div v-if="isAuthenticated" class="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
            <slot />
          </div>
          <slot v-else />
        </main>
      </div>

      <AppSidebar
        v-if="isAuthenticated"
        mobile
        :open="mobileMenuOpen"
        :nav-items="navItems"
        :current-path="route.path"
        :color-mode-value="colorMode.value"
        @close="mobileMenuOpen = false"
        @help="openHelp"
        @accessibility="openAccessibility"
        @toggle-theme="toggleTheme"
        @logout="handleLogout"
      />
    </div>

    <UModal v-model:open="isShowingHelp">
      <template #content>
        <UCard role="dialog" aria-modal="true" aria-labelledby="keyboard-shortcuts-title">
          <template #header>
            <div class="flex items-center justify-between">
              <div class="flex items-center gap-2">
                <UIcon name="i-lucide-keyboard" class="w-5 h-5 text-yellow-400" />
                <h2 id="keyboard-shortcuts-title" class="font-semibold">Keyboard Shortcuts</h2>
              </div>
              <UButton icon="i-lucide-x" variant="ghost" color="neutral" size="xs" aria-label="Close keyboard shortcuts" @click="isShowingHelp = false" />
            </div>
          </template>
          <div class="space-y-2">
            <div
              v-for="shortcut in shortcuts"
              :key="shortcut.key"
              class="flex items-center justify-between py-2 border-b border-gray-100 dark:border-carbon-800 last:border-0"
            >
              <span class="text-sm text-gray-600 dark:text-wire-200/70">{{ shortcut.description }}</span>
              <kbd class="inline-flex items-center gap-1 px-2 py-1 text-xs font-semibold bg-gray-100 dark:bg-carbon-800 border border-gray-300 dark:border-carbon-700 rounded text-gray-700 dark:text-wire-200">
                <template v-for="(part, index) in formatShortcutKey(shortcut.key)" :key="`${shortcut.key}-${index}`">
                  <span v-if="part === '__MAC_CMD_CTRL__'" class="inline-flex items-center gap-1">
                    <span class="text-base leading-none">⌘</span>
                    <span>cmd/Ctrl</span>
                  </span>
                  <span v-else-if="part === '__MAC_CMD__'" class="inline-flex items-center gap-1">
                    <span class="text-base leading-none">⌘</span>
                    <span>cmd</span>
                  </span>
                  <span v-else>{{ part }}</span>
                </template>
              </kbd>
            </div>
          </div>
        </UCard>
      </template>
    </UModal>

    <UModal v-model:open="isShowingAccessibility">
      <template #content>
        <UCard role="dialog" aria-modal="true" aria-labelledby="accessibility-features-title">
          <template #header>
            <div class="flex items-center justify-between">
              <div class="flex items-center gap-2">
                <UIcon name="i-lucide-accessibility" class="w-5 h-5 text-yellow-400" />
                <h2 id="accessibility-features-title" class="font-semibold">Accessibility Features</h2>
              </div>
              <UButton icon="i-lucide-x" variant="ghost" color="neutral" size="xs" aria-label="Close accessibility features" @click="isShowingAccessibility = false" />
            </div>
          </template>
          <div class="space-y-4 text-sm text-gray-600 dark:text-wire-200/70">
            <p>
              wireops includes several accessibility improvements to make navigation easier with keyboard and assistive technologies.
            </p>
            <div class="space-y-3">
              <div class="rounded-lg border border-gray-200 px-4 py-3 dark:border-carbon-800">
                <p class="font-medium text-gray-900 dark:text-wire-200">Keyboard navigation</p>
                <p>Use the skip link, sidebar navigation, and keyboard shortcuts to move through the app without a mouse.</p>
              </div>
              <div class="rounded-lg border border-gray-200 px-4 py-3 dark:border-carbon-800">
                <p class="font-medium text-gray-900 dark:text-wire-200">Visible focus states</p>
                <p>Interactive elements show a clear focus outline so it is easier to track where you are on the page.</p>
              </div>
              <div class="rounded-lg border border-gray-200 px-4 py-3 dark:border-carbon-800">
                <p class="font-medium text-gray-900 dark:text-wire-200">Screen reader support</p>
                <p>Landmarks, labels, dialog titles, and live status announcements help communicate page structure and updates.</p>
              </div>
              <div class="rounded-lg border border-gray-200 px-4 py-3 dark:border-carbon-800">
                <p class="font-medium text-gray-900 dark:text-wire-200">Accessible forms and feedback</p>
                <p>Critical forms and alerts include clearer labels, error states, and descriptive messages for assistive technology.</p>
              </div>
            </div>
          </div>
        </UCard>
      </template>
    </UModal>

    <AppCommandPalette v-if="isAuthenticated" />
  </div>
</template>
