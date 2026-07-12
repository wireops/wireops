<script setup lang="ts">
import { computed, ref, watch } from 'vue'

type NavItem = {
  label: string
  icon: string
  to: string
  children?: NavItem[]
}

const props = defineProps<{
  navItems: NavItem[]
  currentPath: string
  colorModeValue: string
  mobile?: boolean
  open?: boolean
  collapsed?: boolean
}>()

const emit = defineEmits<{
  close: []
  help: []
  accessibility: []
  logout: []
  toggleTheme: []
  toggleCollapse: []
}>()

const { isShowingCommandPalette } = useKeyboard()

const closeButtonRef = ref<{ $el?: HTMLElement } | HTMLElement | null>(null)
const previousFocusedElement = ref<HTMLElement | null>(null)

function isActive(to: string) {
  if (to === '/') return props.currentPath === '/'
  if (to === '/workloads') {
    return props.currentPath.startsWith('/workloads')
      || props.currentPath.startsWith('/stacks')
      || props.currentPath.startsWith('/jobs')
  }
  return props.currentPath.startsWith(to)
}

function hasActiveChild(item: NavItem) {
  return !!item.children?.some(child => isActive(child.to))
}

const expandedMenus = ref<Record<string, boolean>>({})

function isExpanded(item: NavItem) {
  if (!item.children?.length) return false
  return expandedMenus.value[item.to] ?? hasActiveChild(item)
}

function toggleSubmenu(item: NavItem) {
  if (!item.children?.length) return
  expandedMenus.value[item.to] = !isExpanded(item)
}

watch(
  () => props.currentPath,
  () => {
    for (const item of props.navItems) {
      if (item.children?.length && hasActiveChild(item)) {
        expandedMenus.value[item.to] = true
      }
    }
  },
  { immediate: true }
)

const sidebarClasses = computed(() => {
  if (props.mobile) {
    return 'dark relative flex h-full w-full max-w-xs flex-col border-r border-carbon-800 bg-carbon-900 shadow-2xl'
  }

  const width = props.collapsed ? 'lg:w-20' : 'lg:w-72'
  return `dark hidden lg:flex ${width} lg:flex-col lg:border-r lg:border-carbon-800 lg:bg-carbon-900 lg:sticky lg:top-0 lg:h-screen transition-[width] duration-300 ease-in-out z-40`
})

const brandSubtitle = computed(() => props.mobile ? 'Navigation' : 'Control Plane')

function resolveButtonElement(target: { $el?: HTMLElement } | HTMLElement | null) {
  if (!target) return null
  return target instanceof HTMLElement ? target : target.$el ?? null
}

function submenuId(item: NavItem) {
  return `nav-section-${item.to.replace(/[^a-z0-9]+/gi, '-').replace(/^-|-$/g, '').toLowerCase()}`
}

watch(
  () => props.open,
  (isOpen) => {
    if (!props.mobile) return

    if (isOpen) {
      previousFocusedElement.value = document.activeElement instanceof HTMLElement ? document.activeElement : null
      requestAnimationFrame(() => {
        resolveButtonElement(closeButtonRef.value)?.focus()
      })
      return
    }

    previousFocusedElement.value?.focus()
  }
)
</script>

<template>
  <div
    v-if="mobile"
    v-show="open"
    class="fixed inset-0 z-50 lg:hidden"
    aria-labelledby="mobile-navigation-title"
    role="dialog"
    aria-modal="true"
    @keydown.esc.prevent="emit('close')"
  >
    <button
      type="button"
      class="absolute inset-0 bg-carbon-950/55 backdrop-blur-[1px]"
      aria-label="Close menu"
      @click="emit('close')"
    />
    <aside id="mobile-navigation" :class="sidebarClasses">
      <div class="flex items-center justify-between border-b border-carbon-800 px-5 py-5">
        <NuxtLink to="/" class="flex items-center gap-3" aria-label="Go to dashboard" @click="emit('close')">
          <div class="flex h-10 w-10 items-center justify-center rounded-2xl overflow-hidden">
            <img src="~/assets/img/logo.png" alt="wireops" class="h-7 w-7 object-contain">
          </div>
          <div>
            <span class="block font-black text-base tracking-[0.24em] uppercase text-yellow-400">wireops</span>
            <span id="mobile-navigation-title" class="text-xs uppercase tracking-[0.24em] text-wire-200/45">{{ brandSubtitle }}</span>
          </div>
        </NuxtLink>
        <UButton
          ref="closeButtonRef"
          icon="i-lucide-x"
          variant="ghost"
          color="neutral"
          size="sm"
          aria-label="Close navigation menu"
          @click="emit('close')"
        />
      </div>

      <div class="flex flex-1 flex-col px-4 py-6">
        <nav aria-label="Primary navigation" class="space-y-1">
          <UButton
            icon="i-lucide-search"
            label="Search"
            variant="soft"
            color="neutral"
            size="lg"
            class="w-full justify-start mb-4 text-gray-500 dark:text-gray-400"
            @click="isShowingCommandPalette = true; emit('close')"
          >
            <template #trailing>
              <div class="flex items-center gap-1 ml-auto">
                <kbd class="px-1.5 py-0.5 text-[10px] font-semibold text-gray-500 bg-gray-100 dark:bg-carbon-800 border border-gray-200 dark:border-carbon-700 rounded-md shadow-sm">⌘K</kbd>
              </div>
            </template>
          </UButton>
          
          <div v-for="item in navItems" :key="item.to" class="space-y-1">
            <UButton
              v-if="item.children?.length"
              :icon="item.icon"
              :variant="isActive(item.to) || hasActiveChild(item) ? 'soft' : 'ghost'"
              :color="isActive(item.to) || hasActiveChild(item) ? 'primary' : 'neutral'"
              size="lg"
              class="w-full justify-start"
              :aria-label="isExpanded(item) ? `Collapse ${item.label}` : `Expand ${item.label}`"
              :aria-expanded="isExpanded(item)"
              :aria-controls="submenuId(item)"
              @click="toggleSubmenu(item)"
            >
              <span class="flex min-w-0 flex-1 items-center justify-between gap-3">
                <span class="truncate">{{ item.label }}</span>
                <UIcon
                  :name="isExpanded(item) ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
                  class="h-4 w-4 shrink-0"
                />
              </span>
            </UButton>
            <UButton
              v-else
              :to="item.to"
              :icon="item.icon"
              :label="item.label"
              :variant="isActive(item.to) || hasActiveChild(item) ? 'soft' : 'ghost'"
              :color="isActive(item.to) || hasActiveChild(item) ? 'primary' : 'neutral'"
              size="lg"
              class="w-full justify-start"
              :aria-current="isActive(item.to) ? 'page' : undefined"
              @click="emit('close')"
            />

            <div
              v-if="item.children?.length && isExpanded(item)"
              :id="submenuId(item)"
              class="ml-5 space-y-1 border-l border-carbon-800 pl-3"
            >
              <UButton
                v-for="child in item.children"
                :key="child.to"
                :to="child.to"
                :icon="child.icon"
                :label="child.label"
                :variant="isActive(child.to) ? 'soft' : 'ghost'"
                :color="isActive(child.to) ? 'primary' : 'neutral'"
                size="md"
                class="w-full justify-start"
                :aria-current="isActive(child.to) ? 'page' : undefined"
                @click="emit('close')"
              />
            </div>
          </div>
        </nav>

        <div class="mt-auto space-y-3 border-t border-carbon-800 pt-5">
          <UButton
            icon="i-lucide-keyboard"
            label="Keyboard Shortcuts"
            variant="ghost"
            color="neutral"
            size="lg"
            class="w-full justify-start"
            @click="emit('help')"
          />
          <UButton
            icon="i-lucide-accessibility"
            label="Accessibility"
            variant="ghost"
            color="neutral"
            size="lg"
            class="w-full justify-start"
            @click="emit('accessibility')"
          />
          <UButton
            :icon="colorModeValue === 'dark' ? 'i-lucide-sun' : 'i-lucide-moon'"
            :label="colorModeValue === 'dark' ? 'Light Mode' : 'Dark Mode'"
            variant="ghost"
            color="neutral"
            size="lg"
            class="w-full justify-start"
            @click="emit('toggleTheme')"
          />
          <UButton
            icon="i-lucide-log-out"
            label="Logout"
            variant="ghost"
            color="neutral"
            size="lg"
            class="w-full justify-start"
            @click="emit('logout')"
          />
        </div>
      </div>
    </aside>
  </div>

  <aside v-else :class="sidebarClasses">
    <div :class="['flex h-20 items-center border-b border-carbon-800 shrink-0 relative transition-all duration-300', collapsed ? 'justify-center px-0' : 'justify-between px-6']">
      <NuxtLink to="/" class="flex items-center gap-3 shrink-0" aria-label="Go to dashboard">
        <div :class="['flex shrink-0 items-center justify-center rounded-2xl transition-all duration-300 overflow-hidden', collapsed ? 'h-10 w-10' : 'h-11 w-11']">
          <img src="~/assets/img/logo.png" alt="wireops" class="h-8 w-8 object-contain">
        </div>
        <div v-show="!collapsed" class="whitespace-nowrap transition-opacity duration-300">
          <span class="block font-black text-lg tracking-widest uppercase text-yellow-400 drop-shadow-[0_0_8px_rgba(255,198,0,0.4)]">
            wireops
          </span>
          <span class="text-xs uppercase tracking-[0.24em] text-wire-200/45">{{ brandSubtitle }}</span>
        </div>
      </NuxtLink>
    </div>

    <!-- Floating Toggle Button (Used for both Collapsed and Expanded states) -->
    <div class="absolute -right-3 top-[1.6rem] z-50 flex items-center justify-center">
      <UButton
        :icon="collapsed ? 'i-lucide-chevron-right' : 'i-lucide-chevron-left'"
        variant="ghost"
        color="neutral"
        size="xs"
        class="rounded-full shadow-lg border border-carbon-800 bg-carbon-900 text-wire-200/50 hover:text-white hover:bg-carbon-800"
        :aria-label="collapsed ? 'Expand sidebar' : 'Collapse sidebar'"
        :title="collapsed ? 'Expand sidebar' : 'Collapse sidebar'"
        @click="emit('toggleCollapse')"
      />
    </div>

    <div class="flex flex-1 flex-col px-4 py-6 min-h-0">
      <nav aria-label="Primary navigation" class="space-y-1 overflow-y-auto overflow-x-hidden no-scrollbar flex-1 min-h-0 pb-4">
        <UTooltip :text="collapsed ? 'Search' : ''" :prevent="!collapsed" placement="right">
          <UButton
            icon="i-lucide-search"
            :label="collapsed ? undefined : 'Search'"
            :aria-label="collapsed ? 'Search' : undefined"
            variant="soft"
            color="neutral"
            size="lg"
            :class="['w-full mb-4 text-gray-500 dark:text-gray-400 transition-all duration-300', collapsed ? 'justify-center px-0' : 'justify-start']"
            @click="isShowingCommandPalette = true; emit('close')"
          >
            <template v-if="!collapsed" #trailing>
              <div class="flex items-center gap-1 ml-auto">
                <kbd class="px-1.5 py-0.5 text-[10px] font-semibold text-gray-500 bg-gray-100 dark:bg-carbon-800 border border-gray-200 dark:border-carbon-700 rounded-md shadow-sm">⌘K</kbd>
              </div>
            </template>
          </UButton>
        </UTooltip>

        <div v-for="item in navItems" :key="item.to" class="space-y-1">
          <UTooltip :text="collapsed ? item.label : ''" :prevent="!collapsed" placement="right">
            <UButton
              v-if="item.children?.length"
              :icon="item.icon"
              :variant="isActive(item.to) || hasActiveChild(item) ? 'soft' : 'ghost'"
              :color="isActive(item.to) || hasActiveChild(item) ? 'primary' : 'neutral'"
              size="lg"
              :class="['w-full transition-all duration-300', collapsed ? 'justify-center px-0' : 'justify-start']"
              :aria-label="isExpanded(item) ? `Collapse ${item.label}` : `Expand ${item.label}`"
              :aria-expanded="isExpanded(item)"
              :aria-controls="submenuId(item)"
              @click="collapsed ? emit('toggleCollapse') : toggleSubmenu(item)"
            >
              <span v-if="!collapsed" class="flex min-w-0 flex-1 items-center justify-between gap-3">
                <span class="truncate">{{ item.label }}</span>
                <UIcon
                  :name="isExpanded(item) ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
                  class="h-4 w-4 shrink-0"
                />
              </span>
            </UButton>
            <UButton
              v-else
              :to="item.to"
              :icon="item.icon"
              :label="collapsed ? undefined : item.label"
              :aria-label="collapsed ? item.label : undefined"
              :variant="isActive(item.to) || hasActiveChild(item) ? 'soft' : 'ghost'"
              :color="isActive(item.to) || hasActiveChild(item) ? 'primary' : 'neutral'"
              size="lg"
              :class="['w-full transition-all duration-300', collapsed ? 'justify-center px-0' : 'justify-start']"
              :aria-current="isActive(item.to) ? 'page' : undefined"
            />
          </UTooltip>

          <div
            v-if="item.children?.length && isExpanded(item) && !collapsed"
            :id="submenuId(item)"
            class="ml-5 space-y-1 border-l border-carbon-800 pl-3"
          >
            <UButton
              v-for="child in item.children"
              :key="child.to"
              :to="child.to"
              :icon="child.icon"
              :label="child.label"
              :variant="isActive(child.to) ? 'soft' : 'ghost'"
              :color="isActive(child.to) ? 'primary' : 'neutral'"
              size="md"
              class="w-full justify-start"
              :aria-current="isActive(child.to) ? 'page' : undefined"
            />
          </div>
        </div>
      </nav>

      <div class="mt-auto space-y-3 border-t border-carbon-800 pt-5 overflow-hidden shrink-0">
        <UTooltip :text="collapsed ? 'Keyboard Shortcuts' : ''" :prevent="!collapsed" placement="right">
          <UButton
            icon="i-lucide-keyboard"
            :label="collapsed ? undefined : 'Keyboard Shortcuts'"
            :aria-label="collapsed ? 'Keyboard Shortcuts' : undefined"
            variant="ghost"
            color="neutral"
            size="lg"
            :class="['w-full transition-all duration-300', collapsed ? 'justify-center px-0' : 'justify-start']"
            @click="emit('help')"
          />
        </UTooltip>
        <UTooltip :text="collapsed ? 'Accessibility' : ''" :prevent="!collapsed" placement="right">
          <UButton
            icon="i-lucide-accessibility"
            :label="collapsed ? undefined : 'Accessibility'"
            :aria-label="collapsed ? 'Accessibility' : undefined"
            variant="ghost"
            color="neutral"
            size="lg"
            :class="['w-full transition-all duration-300', collapsed ? 'justify-center px-0' : 'justify-start']"
            @click="emit('accessibility')"
          />
        </UTooltip>
        <UTooltip :text="collapsed ? (colorModeValue === 'dark' ? 'Light Mode' : 'Dark Mode') : ''" :prevent="!collapsed" placement="right">
          <UButton
            :icon="colorModeValue === 'dark' ? 'i-lucide-sun' : 'i-lucide-moon'"
            :label="collapsed ? undefined : (colorModeValue === 'dark' ? 'Light Mode' : 'Dark Mode')"
            :aria-label="collapsed ? (colorModeValue === 'dark' ? 'Light Mode' : 'Dark Mode') : undefined"
            variant="ghost"
            color="neutral"
            size="lg"
            :class="['w-full transition-all duration-300', collapsed ? 'justify-center px-0' : 'justify-start']"
            @click="emit('toggleTheme')"
          />
        </UTooltip>
        <UTooltip :text="collapsed ? 'Logout' : ''" :prevent="!collapsed" placement="right">
          <UButton
            icon="i-lucide-log-out"
            :label="collapsed ? undefined : 'Logout'"
            :aria-label="collapsed ? 'Logout' : undefined"
            variant="ghost"
            color="neutral"
            size="lg"
            :class="['w-full transition-all duration-300', collapsed ? 'justify-center px-0' : 'justify-start']"
            @click="emit('logout')"
          />
        </UTooltip>
      </div>
    </div>
  </aside>
</template>
