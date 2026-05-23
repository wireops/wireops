<script setup lang="ts">
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
  logout: []
  toggleTheme: []
}>()

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
</script>

<template>
  <div
    v-if="mobile"
    v-show="open"
    class="fixed inset-0 z-50 lg:hidden"
    aria-label="Mobile navigation"
    role="dialog"
    aria-modal="true"
  >
    <button
      type="button"
      class="absolute inset-0 bg-carbon-950/55 backdrop-blur-[1px]"
      aria-label="Close menu"
      @click="emit('close')"
    />
    <aside :class="sidebarClasses">
      <div class="flex items-center justify-between border-b border-carbon-800 px-5 py-5">
        <NuxtLink to="/" class="flex items-center gap-3" @click="emit('close')">
          <div class="flex h-10 w-10 items-center justify-center rounded-2xl bg-yellow-400/10 ring-1 ring-yellow-400/20">
            <UIcon name="i-lucide-zap" class="h-5 w-5 text-yellow-400" />
          </div>
          <div>
            <span class="block font-black text-base tracking-[0.24em] uppercase text-yellow-400">wireops</span>
            <span class="text-xs uppercase tracking-[0.24em] text-wire-200/45">{{ brandSubtitle }}</span>
          </div>
        </NuxtLink>
        <UButton
          icon="i-lucide-x"
          variant="ghost"
          color="neutral"
          size="sm"
          @click="emit('close')"
        />
      </div>

      <div class="flex flex-1 flex-col px-4 py-6">
        <nav class="space-y-1">
          <div v-for="item in navItems" :key="item.to" class="space-y-1">
            <UButton
              v-if="item.children?.length"
              :icon="item.icon"
              :variant="isActive(item.to) || hasActiveChild(item) ? 'soft' : 'ghost'"
              :color="isActive(item.to) || hasActiveChild(item) ? 'primary' : 'neutral'"
              size="lg"
              class="w-full justify-start"
              :aria-label="isExpanded(item) ? `Collapse ${item.label}` : `Expand ${item.label}`"
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
              @click="emit('close')"
            />

            <div
              v-if="item.children?.length && isExpanded(item)"
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
      <NuxtLink to="/" class="flex items-center gap-3">
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
      <nav class="space-y-1">
        <div v-for="item in navItems" :key="item.to" class="space-y-1">
          <UButton
            v-if="item.children?.length"
            :icon="item.icon"
            :variant="isActive(item.to) || hasActiveChild(item) ? 'soft' : 'ghost'"
            :color="isActive(item.to) || hasActiveChild(item) ? 'primary' : 'neutral'"
            size="lg"
            class="w-full justify-start"
            :aria-label="isExpanded(item) ? `Collapse ${item.label}` : `Expand ${item.label}`"
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
          />

          <div
            v-if="item.children?.length && isExpanded(item)"
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
