<script setup lang="ts">
import { ref } from 'vue'

type EnvVarCollection = 'stack_env_vars' | 'job_env_vars' | 'global_env_vars'

const props = defineProps<{
  collection: EnvVarCollection
  envVarId: string
  icon?: string
  title?: string
}>()

const { revealEnvVar } = useApi()

const revealed = ref(false)
const loading = ref(false)
const error = ref('')
const plaintext = ref('')

async function toggle() {
  if (revealed.value) {
    revealed.value = false
    plaintext.value = ''
    error.value = ''
    return
  }
  loading.value = true
  error.value = ''
  try {
    const res = await revealEnvVar(props.collection, props.envVarId)
    plaintext.value = res.value
    revealed.value = true
  } catch (e: any) {
    error.value = e?.data?.error || e?.message || 'Failed to reveal value'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="flex flex-col gap-1 min-w-0">
    <AppTextInput
      :model-value="revealed ? plaintext : '••••••••'"
      disabled
      :type="revealed ? 'text' : 'password'"
      :icon="icon"
      :title="title"
      class="font-mono"
    >
      <template #trailing>
        <AppButtonInput
          :icon="revealed ? 'i-lucide-eye-off' : 'i-lucide-eye'"
          :aria-label="revealed ? 'Hide value' : 'Reveal value'"
          size="sm"
          :disabled="loading"
          @click="toggle"
        />
      </template>
    </AppTextInput>
    <p v-if="error" class="text-xs text-red-600 dark:text-red-400">{{ error }}</p>
  </div>
</template>
