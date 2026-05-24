<script setup lang="ts">
const open = defineModel<boolean>('open', { default: false })

const props = defineProps<{
  title: string
  icon?: string
  titleClass?: string
  chevronClass?: string
}>()
</script>

<template>
  <UCard v-if="open">
    <template #header>
      <button
        type="button"
        class="flex w-full items-center justify-between gap-3 text-left"
        :aria-expanded="open"
        @click="open = !open"
      >
        <div class="flex items-center gap-2">
          <UIcon v-if="props.icon" :name="props.icon" class="h-5 w-5" />
          <h3 class="font-semibold" :class="props.titleClass">{{ props.title }}</h3>
        </div>
        <UIcon
          name="i-lucide-chevron-down"
          class="h-4 w-4 transition-transform"
          :class="[open ? 'rotate-180' : '', props.chevronClass]"
        />
      </button>
    </template>

    <slot />
  </UCard>

  <UCard v-else>
    <template #header>
      <button
        type="button"
        class="flex w-full items-center justify-between gap-3 text-left"
        :aria-expanded="open"
        @click="open = !open"
      >
        <div class="flex items-center gap-2">
          <UIcon v-if="props.icon" :name="props.icon" class="h-5 w-5" />
          <h3 class="font-semibold" :class="props.titleClass">{{ props.title }}</h3>
        </div>
        <UIcon
          name="i-lucide-chevron-down"
          class="h-4 w-4 transition-transform"
          :class="[open ? 'rotate-180' : '', props.chevronClass]"
        />
      </button>
    </template>
  </UCard>
</template>
