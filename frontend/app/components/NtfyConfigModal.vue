<script setup lang="ts">
import { ref, watch } from 'vue'

const props = defineProps<{
  integration: any
}>()

const emit = defineEmits<{
  saved: []
}>()

const isOpen = defineModel<boolean>('open', { default: false })
const loading = ref(false)
const testLoading = ref(false)
const hasSecret = ref(false)

const allEvents = [
  { value: 'sync.started', label: 'Started' },
  { value: 'sync.done',    label: 'Done' },
  { value: 'sync.error',   label: 'Error' },
]

const form = ref({
  url: 'https://ntfy.sh',
  topic: '',
  user: '',
  secret: '',
  template: '',
  events: ['sync.started', 'sync.done', 'sync.error'] as string[],
})

watch(() => props.integration, (newVal) => {
  if (newVal) {
    const config = newVal.config || {}
    form.value.url = config.url || 'https://ntfy.sh'
    form.value.topic = config.topic || ''
    form.value.user = config.user || ''
    form.value.secret = config.secret || ''
    form.value.template = config.template || ''
    form.value.events = config.events || ['sync.started', 'sync.done', 'sync.error']
    hasSecret.value = config.secret === '••••••••'
  }
}, { immediate: true, deep: true })

const toast = useToast()
const { saveIntegration, testIntegration } = useIntegrations()

function close() {
  isOpen.value = false
}

function toggleEvent(event: string) {
  const idx = form.value.events.indexOf(event)
  if (idx >= 0) {
    form.value.events.splice(idx, 1)
  } else {
    form.value.events.push(event)
  }
}

function onSecretFocus() {
  if (hasSecret.value && form.value.secret === '••••••••') {
    form.value.secret = ''
    hasSecret.value = false
  }
}

async function handleSave() {
  if (!form.value.topic) {
    toast.add({ title: 'Topic is required', color: 'error' })
    return
  }
  loading.value = true
  try {
    const success = await saveIntegration('ntfy', props.integration.enabled, {
      url: form.value.url,
      topic: form.value.topic,
      user: form.value.user,
      secret: form.value.secret,
      template: form.value.template,
      events: form.value.events,
    })
    if (success) {
      toast.add({ title: 'Ntfy integration saved', color: 'success' })
      emit('saved')
      close()
    } else {
      toast.add({ title: 'Failed to save settings', color: 'error' })
    }
  } catch (e: any) {
    toast.add({ title: 'Error saving settings', description: e.message, color: 'error' })
  } finally {
    loading.value = false
  }
}

async function handleTest() {
  if (!form.value.topic) {
    toast.add({ title: 'Topic is required to test', color: 'error' })
    return
  }
  testLoading.value = true
  try {
    await testIntegration('ntfy', {
      url: form.value.url,
      topic: form.value.topic,
      user: form.value.user,
      secret: form.value.secret,
      template: form.value.template,
      events: form.value.events,
    })
    toast.add({ title: 'Test event dispatched', description: 'Check your Ntfy channel', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Test failed', description: e.message, color: 'error' })
  } finally {
    testLoading.value = false
  }
}
</script>

<template>
  <UModal
    v-model:open="isOpen"
    title="Configure Ntfy Integration"
    description="Configure and test your Ntfy push notification channel."
  >
    <template #body>
      <div class="space-y-4" role="document">
        <div>
          <label class="block text-sm font-medium mb-2">Subscribe Events</label>
          <div class="flex flex-wrap gap-3">
            <label
              v-for="event in allEvents"
              :key="event.value"
              class="flex items-center gap-2 cursor-pointer select-none"
            >
              <UCheckbox
                :model-value="form.events.includes(event.value)"
                @update:model-value="toggleEvent(event.value)"
              />
              <span class="text-sm font-mono">{{ event.label }}</span>
            </label>
          </div>
        </div>

        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <UFormField label="Server URL" required>
            <UInput v-model="form.url" placeholder="https://ntfy.sh" class="w-full font-mono text-sm" />
          </UFormField>
          <UFormField label="Topic" required>
            <UInput v-model="form.topic" placeholder="my-topic" class="w-full font-mono text-sm" />
          </UFormField>
        </div>

        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <UFormField label="Username (optional)">
            <UInput v-model="form.user" placeholder="user" class="w-full font-mono text-sm" />
          </UFormField>
          <UFormField label="Password (optional)">
            <UInput
              v-model="form.secret"
              :type="hasSecret && form.secret === '••••••••' ? 'password' : 'text'"
              placeholder="password"
              class="w-full font-mono text-sm"
              @focus="onSecretFocus"
            />
          </UFormField>
        </div>

        <UFormField label="Custom Template (optional)">
          <UTextarea
            v-model="form.template"
            placeholder="Event: {{.Event}}&#10;Stack: {{.StackName}}&#10;Trigger: {{.Trigger}}&#10;Commit: {{.CommitSHA}}&#10;{{if .Error}}Error: {{.Error}}{{end}}"
            :rows="5"
            class="w-full font-mono text-sm"
          />
          <p v-pre class="text-[11px] text-gray-400 mt-1">
            Supports Go templates. Variables: <code>{{.StackName}}</code>, <code>{{.Event}}</code>, <code>{{.Trigger}}</code>, <code>{{.CommitSHA}}</code>, <code>{{.DurationMs}}</code>, <code>{{.Error}}</code>.
          </p>
        </UFormField>
      </div>
    </template>

    <template #footer>
      <div class="flex w-full items-center gap-2">
        <UButton label="Cancel" variant="outline" @click="close" />
        <UButton
          label="Send Test"
          icon="i-lucide-send"
          variant="subtle"
          color="neutral"
          :loading="testLoading"
          :disabled="!form.topic"
          @click="handleTest"
        />
        <UButton
          label="Save Settings"
          color="primary"
          class="ml-auto"
          :loading="loading"
          @click="handleSave"
        />
      </div>
    </template>
  </UModal>
</template>
