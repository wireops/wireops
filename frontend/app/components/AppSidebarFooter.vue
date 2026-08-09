<script setup lang="ts">
withDefaults(defineProps<{
  colorModeValue: string
  accountActive?: boolean
  collapsed?: boolean
}>(), {
  accountActive: false,
  collapsed: false,
})

const emit = defineEmits<{
  help: []
  accessibility: []
  toggleTheme: []
  logout: []
}>()
</script>

<template>
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

    <div class="flex items-center gap-1">
      <UTooltip :text="collapsed ? 'Accessibility' : ''" :prevent="!collapsed" placement="right" class="flex-1 min-w-0">
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
      <UTooltip v-if="!collapsed" :text="colorModeValue === 'dark' ? 'Light Mode' : 'Dark Mode'" placement="right">
        <UButton
          :icon="colorModeValue === 'dark' ? 'i-lucide-sun' : 'i-lucide-moon'"
          :aria-label="colorModeValue === 'dark' ? 'Light Mode' : 'Dark Mode'"
          variant="ghost"
          color="neutral"
          size="lg"
          class="justify-center shrink-0 px-3"
          @click="emit('toggleTheme')"
        />
      </UTooltip>
    </div>
    <UTooltip v-if="collapsed" :text="colorModeValue === 'dark' ? 'Light Mode' : 'Dark Mode'" placement="right">
      <UButton
        :icon="colorModeValue === 'dark' ? 'i-lucide-sun' : 'i-lucide-moon'"
        :aria-label="colorModeValue === 'dark' ? 'Light Mode' : 'Dark Mode'"
        variant="ghost"
        color="neutral"
        size="lg"
        class="w-full justify-center px-0"
        @click="emit('toggleTheme')"
      />
    </UTooltip>

    <div class="flex items-center gap-1">
      <UTooltip :text="collapsed ? 'Account' : ''" :prevent="!collapsed" placement="right" class="flex-1 min-w-0">
        <UButton
          icon="i-lucide-user-circle"
          :label="collapsed ? undefined : 'Account'"
          :aria-label="collapsed ? 'Account' : undefined"
          variant="ghost"
          color="neutral"
          size="lg"
          :class="['w-full transition-all duration-300', collapsed ? 'justify-center px-0' : 'justify-start']"
          to="/account"
          :aria-current="accountActive ? 'page' : undefined"
        />
      </UTooltip>
      <UTooltip v-if="!collapsed" text="Logout" placement="right">
        <UButton
          icon="i-lucide-log-out"
          aria-label="Logout"
          variant="ghost"
          color="neutral"
          size="lg"
          class="justify-center shrink-0 px-3"
          @click="emit('logout')"
        />
      </UTooltip>
    </div>
    <UTooltip v-if="collapsed" text="Logout" placement="right">
      <UButton
        icon="i-lucide-log-out"
        aria-label="Logout"
        variant="ghost"
        color="neutral"
        size="lg"
        class="w-full justify-center px-0"
        @click="emit('logout')"
      />
    </UTooltip>
  </div>
</template>
