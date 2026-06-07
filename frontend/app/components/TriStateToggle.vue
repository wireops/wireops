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
      @click="toggleBoolean"
      :title="model === true ? 'Enabled' : 'Disabled'"
      class="rounded-md px-3 transition-all"
    />
    <UButton
      icon="i-lucide-globe"
      :color="model === null ? 'primary' : 'gray'"
      :variant="model === null ? 'solid' : 'ghost'"
      @click="select(null)"
      title="Inherit from global policy"
      class="rounded-md px-3"
    />
  </div>
</template>
