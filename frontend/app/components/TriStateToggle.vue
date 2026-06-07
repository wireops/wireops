<script setup lang="ts">
const model = defineModel<boolean | null>({ required: true })
const emit = defineEmits<{ (e: 'change', value: boolean | null): void }>()

function select(value: boolean | null) {
  model.value = value
  emit('change', value)
}

function toggleBoolean() {
  select(model.value === true ? false : true)
}
</script>

<template>
  <div class="flex items-center p-1 bg-gray-100 dark:bg-carbon-800 rounded-lg shadow-inner">
    <UButton
      :icon="model === true ? 'i-lucide-power' : 'i-lucide-power-off'"
      :color="model === true ? 'primary' : 'gray'"
      :variant="model !== null ? 'solid' : 'ghost'"
      :title="model === true ? 'Enabled' : 'Disabled'"
      :aria-label="model === true ? 'Disable' : 'Enable'"
      class="rounded-md px-3 transition-all"
      @click="toggleBoolean"
    />
    <UButton
      icon="i-lucide-globe"
      :color="model === null ? 'primary' : 'gray'"
      :variant="model === null ? 'solid' : 'ghost'"
      title="Inherit from global policy"
      aria-label="Inherit from global policy"
      class="rounded-md px-3"
      @click="select(null)"
    />
  </div>
</template>
