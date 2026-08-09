<script setup lang="ts">
import { ref } from 'vue'

const emit = defineEmits<{
  submit: [form: { name: string; description: string; role: string }]
  cancel: []
}>()

const form = ref({
  name: '',
  description: '',
  role: 'viewer'
})

const roleOptions = [
  { label: 'Viewer', value: 'viewer' },
  { label: 'Operator', value: 'operator' },
]

function reset() {
  form.value = { name: '', description: '', role: 'viewer' }
}

function handleSubmit() {
  emit('submit', { ...form.value })
}

function handleCancel() {
  emit('cancel')
  reset()
}

defineExpose({ reset })
</script>

<template>
  <AppPanelCard :ui="{ body: 'p-6' }">
    <template #header>
      <div class="flex items-center gap-2">
        <UIcon name="i-lucide-key-round" class="w-5 h-5 text-amber-500" />
        <h2 class="font-semibold text-lg text-gray-900 dark:text-white">Create Service Account</h2>
      </div>
      <p class="text-xs text-gray-500 mt-1">
        Create a programmatic account for automation, agents, and external API access.
      </p>
    </template>

    <form class="space-y-4" @submit.prevent="handleSubmit">
      <UFormField label="Name" required>
        <AppTextInput v-model="form.name" placeholder="e.g. CI-CD-deployer" />
      </UFormField>

      <UFormField label="Description" required>
        <AppTextInput v-model="form.description" placeholder="What is this service account for?" />
      </UFormField>

      <UFormField label="Role">
        <AppSelectInput v-model="form.role" :items="roleOptions" />
      </UFormField>

      <div class="flex justify-end gap-2 pt-2">
        <CancelButton @click="handleCancel" />
        <ActionButton type="submit" label="Create" icon="i-lucide-plus" :glow="false" />
      </div>
    </form>
  </AppPanelCard>
</template>
