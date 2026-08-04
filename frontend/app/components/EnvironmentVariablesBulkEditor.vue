<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { parseEnvFileContent, serializeEnvLines, type ParsedEnvLine } from '../utils/envFileParser'
import { isInternalSecret } from '../utils/envVarSecrets'

type TargetType = 'stack' | 'job'

const props = defineProps<{
  targetType: TargetType
  targetId: string
  envVars: any[]
  // When set, prefills the textarea with this content instead of envVars
  // (the .env import flow) and shows a "Replace all" toggle defaulting to
  // off (append/update only) rather than always replacing the full set.
  importContent?: string
  // Keys that should default to secret=true when saved (e.g. inserted from
  // a template) — the KEY=VALUE textarea has no way to carry that flag, so
  // it's passed alongside separately. Only applies to keys with no existing
  // record yet.
  defaultSecretKeys?: Set<string>
}>()

const emit = defineEmits<{
  saved: []
  cancel: []
}>()

const { customPost } = useApi()
const toast = useToast()

function buildInitialContent(): string {
  if (props.importContent !== undefined) return props.importContent
  const lines: ParsedEnvLine[] = props.envVars.map(env => ({
    key: env.key,
    value: isInternalSecret(env) ? '' : (env.value ?? ''),
  }))
  return serializeEnvLines(lines)
}

const initialContent = ref(buildInitialContent())
const text = ref(initialContent.value)
const replaceAll = ref(props.importContent === undefined)
const saving = ref(false)

// Only refresh the textarea from incoming props (e.g. a realtime update to
// envVars from another tab) when the user hasn't started editing — otherwise
// an in-progress, unsaved edit would be silently overwritten.
watch(() => [props.envVars, props.importContent], () => {
  const next = buildInitialContent()
  if (text.value === initialContent.value) {
    text.value = next
  }
  initialContent.value = next
})

const parsed = computed(() => parseEnvFileContent(text.value))

const existingByKey = computed(() => {
  const map = new Map<string, any>()
  for (const env of props.envVars) map.set(env.key, env)
  return map
})

async function submit() {
  if (parsed.value.errors.length > 0) return
  saving.value = true
  try {
    const vars = parsed.value.vars.map(({ key, value }) => {
      const existing = existingByKey.value.get(key)
      const secret = existing?.secret ?? props.defaultSecretKeys?.has(key) ?? false
      const secretProvider = existing?.secret_provider ?? (secret ? 'internal' : '')
      // A blank value for an existing internal-provider secret means
      // "unchanged" — the browser never receives the decrypted value to
      // round-trip, so an empty line for that key must not wipe it.
      return { key, value, secret, secret_provider: secretProvider }
    })
    // Only stacks have a bulk-upsert route today (job_env_vars can get the
    // same treatment later); EnvironmentVariablesCard only renders bulk
    // edit/import for targetType === 'stack'.
    await customPost(`/api/custom/stacks/${props.targetId}/env-vars/bulk`, {
      mode: replaceAll.value ? 'replace' : 'append',
      vars,
    })
    toast.add({ title: 'Environment variables saved', color: 'success' })
    emit('saved')
  } catch (error: any) {
    toast.add({ title: 'Failed to save environment variables', description: error?.data?.error || error?.message, color: 'error' })
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="space-y-3">
    <p class="text-xs text-gray-500">
      One <code class="font-mono">KEY=VALUE</code> per line. Leave a secret's value blank to keep it unchanged.
    </p>

    <UTextarea
      v-model="text"
      :rows="10"
      class="w-full font-mono text-xs"
      placeholder="KEY=value"
      autoresize
    />

    <div v-if="parsed.errors.length" class="space-y-1 rounded-md bg-red-500/10 p-2 text-xs text-red-600 dark:text-red-400">
      <p v-for="err in parsed.errors" :key="err.line">
        Line {{ err.line }}: {{ err.message }}
      </p>
    </div>

    <UFormField v-if="importContent !== undefined" label="">
      <label class="flex items-center gap-2 text-xs text-gray-500">
        <input v-model="replaceAll" type="checkbox">
        Replace all existing variables (instead of adding/updating only)
      </label>
    </UFormField>

    <div class="flex justify-end gap-2">
      <CancelButton @click="emit('cancel')" />
      <UButton
        label="Save"
        icon="i-lucide-check"
        color="success"
        :loading="saving"
        :disabled="parsed.errors.length > 0 || parsed.vars.length === 0"
        @click="submit"
      />
    </div>
  </div>
</template>
