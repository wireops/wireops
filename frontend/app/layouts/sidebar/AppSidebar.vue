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
}>()

const emit = defineEmits<{
  close: []
  help: []
  accessibility: []
  logout: []
  toggleTheme: []
}>()

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

  return 'dark hidden lg:flex lg:w-72 lg:flex-col lg:border-r lg:border-carbon-800 lg:bg-carbon-900'
})

const brandSubtitle = computed(() => props.mobile ? 'Navigation' : 'Control Center')

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
          <div class="flex h-10 w-10 items-center justify-center rounded-2xl bg-yellow-400/10 ring-1 ring-yellow-400/20">
            <UIcon name="i-lucide-zap" class="h-5 w-5 text-yellow-400" />
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
    <div class="flex h-20 items-center border-b border-carbon-800 px-6">
      <NuxtLink to="/" class="flex items-center gap-3" aria-label="Go to dashboard">
        <div class="flex h-11 w-11 items-center justify-center rounded-2xl bg-yellow-400/10 ring-1 ring-yellow-400/20">
          <UIcon name="i-lucide-zap" class="h-6 w-6 text-yellow-400 drop-shadow-[0_0_6px_rgba(255,198,0,0.6)]" />
        </div>
        <div>
          <span class="block font-black text-lg tracking-widest uppercase text-yellow-400 drop-shadow-[0_0_8px_rgba(255,198,0,0.4)]">
            wireops
          </span>
          <span class="text-xs uppercase tracking-[0.24em] text-wire-200/45">{{ brandSubtitle }}</span>
        </div>
      </NuxtLink>
    </div>

    <div class="flex flex-1 flex-col px-4 py-6">
      <nav aria-label="Primary navigation" class="space-y-1">
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
</template>
