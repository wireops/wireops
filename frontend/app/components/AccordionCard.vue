<script setup lang="ts">
const open = defineModel<boolean>('open', { default: false })

const props = defineProps<{
  title: string
  icon?: string
  iconClass?: string
  titleClass?: string
  chevronClass?: string
}>()

const rootEl = ref<HTMLElement | null>(null)

// Deliberate, not a side effect of DOM churn: when the panel opens, bring
// it fully into view instead of leaving the user to scroll manually.
watch(open, (isOpen) => {
  if (!isOpen) return
  nextTick(() => {
    rootEl.value?.scrollIntoView({ behavior: 'smooth', block: 'end' })
  })
})
</script>

<template>
  <div ref="rootEl">
    <UCard>
      <template #header>
        <button
          type="button"
          class="flex w-full items-center justify-between gap-3 text-left cursor-pointer"
          :aria-expanded="open"
          @click="open = !open"
        >
          <div class="flex items-center gap-2">
            <UIcon v-if="props.icon" :name="props.icon" class="h-5 w-5" :class="props.iconClass" />
            <h3 class="font-semibold" :class="props.titleClass">{{ props.title }}</h3>
          </div>
          <UIcon
            name="i-lucide-chevron-down"
            class="h-4 w-4 transition-transform"
            :class="[open ? 'rotate-180' : '', props.chevronClass]"
          />
        </button>
      </template>

      <template v-if="open" #default>
        <slot />
      </template>
    </UCard>
  </div>
</template>
