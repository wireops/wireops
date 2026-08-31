<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { isValidEnvKey, parseEnvFileContent, type PendingEnvVar } from '../utils/envFileParser'

const rows = defineModel<PendingEnvVar[]>({ default: () => [] })

const toast = useToast()
const { load: loadProviderOptions, providerOptions, hasActiveBackends, iconFor, avatarFor } = useSecretProviderOptions()

onMounted(() => {
  loadProviderOptions()
})

const viewMode = ref<'rows' | 'paste'>('rows')
const pasteContent = ref('')
const pasteErrors = ref<string[]>([])

const newKey = ref('')
const newValue = ref('')
const newSecret = ref(false)
const newProvider = ref('internal')

const existingKeys = computed(() => new Set(rows.value.map(r => r.key)))

const newKeyError = computed(() => {
  const key = newKey.value.trim()
  if (!key) return ''
  if (!isValidEnvKey(key)) return 'Invalid key format'
  if (existingKeys.value.has(key)) return 'Key already added'
  return ''
})

function resetNewRow() {
  newKey.value = ''
  newValue.value = ''
  newSecret.value = false
  newProvider.value = 'internal'
}

function addRow() {
  const key = newKey.value.trim()
  if (!key || newKeyError.value) return
  rows.value = [
    ...rows.value,
    { key, value: newValue.value, secret: newSecret.value, secret_provider: newSecret.value ? newProvider.value : '' },
  ]
  resetNewRow()
}

function removeRow(index: number) {
  rows.value = rows.value.filter((_, i) => i !== index)
}

// Commits the in-progress key/value draft row (if any) so it isn't
// silently lost when the wizard advances without clicking the row's
// own add button.
function commitDraft() {
  if (newKey.value.trim() && newValue.value.trim() && !newKeyError.value) {
    addRow()
  }
}

// True when the draft key can't be committed because it's invalid or a
// duplicate — callers use this to block navigation instead of silently
// dropping it via commitDraft() above.
const hasInvalidDraft = computed(() => !!newKey.value.trim() && !!newKeyError.value)

defineExpose({ commitDraft, hasInvalidDraft })

function openPasteMode() {
  pasteContent.value = ''
  pasteErrors.value = []
  viewMode.value = 'paste'
}

function closePasteMode() {
  viewMode.value = 'rows'
}

function applyPaste() {
  const { vars, errors } = parseEnvFileContent(pasteContent.value)
  pasteErrors.value = errors.map(e => `Line ${e.line}: ${e.message}`)
  if (!vars.length) return

  const merged = new Map(rows.value.map(r => [r.key, r]))
  for (const v of vars) {
    merged.set(v.key, { key: v.key, value: v.value, secret: false, secret_provider: '' })
  }
  rows.value = Array.from(merged.values())
  toast.add({ title: `Added ${vars.length} variable${vars.length === 1 ? '' : 's'}`, color: 'success' })
  // Partial import: keep paste mode open so pasteErrors stays visible
  // instead of silently discarding the lines that failed to parse.
  if (!errors.length) closePasteMode()
}

function triggerFileImport() {
  fileInput.value?.click()
}

const fileInput = ref<HTMLInputElement | null>(null)

// Mirrors EnvironmentVariablesCard.vue's isAllowedImportFile — accept lists
// via JS, not the input's `accept` attribute, since macOS's picker hides
// dotfiles (including .env itself) when `accept` is set.
function isAllowedImportFile(name: string) {
  return /(^|\/)\.env(\..+)?$/i.test(name) || /\.(env|txt)$/i.test(name)
}

function onImportFileSelected(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  if (!isAllowedImportFile(file.name)) {
    toast.add({ title: 'Unsupported file', description: 'Only .env and .txt files can be imported.', color: 'error' })
    ;(e.target as HTMLInputElement).value = ''
    return
  }
  const reader = new FileReader()
  reader.onload = () => {
    pasteContent.value = String(reader.result || '')
    pasteErrors.value = []
    viewMode.value = 'paste'
  }
  reader.readAsText(file)
  ;(e.target as HTMLInputElement).value = ''
}
</script>

<template>
  <div class="space-y-3">
    <div class="flex items-center justify-between gap-2">
      <p class="text-xs text-gray-500 dark:text-wire-200/60">
        Optional — add variables now, or skip this step and add them later.
      </p>
      <div class="flex items-center gap-2">
        <input ref="fileInput" type="file" class="hidden" @change="onImportFileSelected">
        <AppButtonInput
          :icon="viewMode === 'paste' ? 'i-lucide-list' : 'i-lucide-text-cursor-input'"
          :label="viewMode === 'paste' ? 'Rows' : 'Paste .env'"
          @click="viewMode === 'paste' ? closePasteMode() : openPasteMode()"
        />
        <AppButtonInput icon="i-lucide-upload" label="Import" @click="triggerFileImport" />
      </div>
    </div>

    <div v-if="viewMode === 'paste'" class="space-y-2">
      <UTextarea
        v-model="pasteContent"
        placeholder="KEY=value&#10;ANOTHER_KEY=value"
        class="w-full font-mono"
        :rows="8"
      />
      <ul v-if="pasteErrors.length" class="text-xs text-red-500 space-y-0.5">
        <li v-for="(err, i) in pasteErrors" :key="i">{{ err }}</li>
      </ul>
      <div class="flex justify-end gap-2">
        <CancelButton @click="closePasteMode" />
        <UButton label="Add variables" icon="i-lucide-check" color="success" :disabled="!pasteContent.trim()" @click="applyPaste" />
      </div>
    </div>

    <div v-else class="space-y-2">
      <div v-if="!rows.length" class="rounded-lg border border-dashed border-gray-300 dark:border-wire-700 px-3 py-4 text-center text-sm text-gray-500">
        No environment variables added — this step is optional, click Next to skip.
      </div>

      <div v-for="(row, index) in rows" :key="`${row.key}-${index}`" class="grid grid-cols-1 gap-2 py-1 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem] sm:items-center">
        <AppTextInput :model-value="row.key" disabled class="font-mono" />
        <AppTextInput
          v-if="row.secret"
          model-value="••••••••"
          disabled
          type="password"
          :icon="iconFor(row.secret_provider || 'internal')"
          class="font-mono"
        />
        <AppTextInput v-else :model-value="row.value" disabled class="font-mono" />
        <UButton
          icon="i-lucide-trash-2"
          variant="ghost"
          color="error"
          size="xs"
          class="h-8 w-full justify-center !bg-red-500/10 p-0 !text-red-600 hover:!bg-red-500/15 sm:w-8 sm:!bg-transparent sm:!text-inherit sm:hover:!bg-transparent dark:!text-red-400"
          aria-label="Remove environment variable"
          @click="removeRow(index)"
        />
      </div>

      <p v-if="newKeyError" class="text-xs text-red-500">{{ newKeyError }}</p>
      <form class="grid grid-cols-1 gap-2 pt-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem_2rem] sm:items-center" @submit.prevent="addRow">
        <AppTextInput v-model="newKey" placeholder="KEY" class="font-mono" />
        <div class="flex items-center gap-1">
          <AppSelectInput
            v-if="newSecret && hasActiveBackends"
            v-model="newProvider"
            :items="providerOptions"
            :searchable="false"
            content-width
            class="font-mono shrink-0"
          />
          <IntegrationsVaultReferencePicker v-if="newSecret && newProvider === 'vault'" v-model="newValue" />
          <IntegrationsInfisicalReferencePicker v-else-if="newSecret && newProvider === 'infisical'" v-model="newValue" />
          <AppTextInput
            v-else
            v-model="newValue"
            placeholder="value"
            :type="newSecret ? 'password' : 'text'"
            :icon="newSecret ? iconFor(newProvider) : undefined"
            :avatar="newSecret ? avatarFor(newProvider) : undefined"
            class="font-mono w-full"
          />
        </div>
        <UButton
          type="button"
          :icon="newSecret ? 'i-lucide-lock' : 'i-lucide-variable'"
          :color="newSecret ? 'warning' : 'neutral'"
          variant="soft"
          size="xs"
          class="h-8 w-8 justify-center p-0"
          :aria-pressed="newSecret"
          :aria-label="newSecret ? 'Set as plain text' : 'Set as secret'"
          :title="newSecret ? 'Secret' : 'Plain text'"
          @click="newSecret = !newSecret"
        />
        <UButton
          type="submit"
          icon="i-lucide-plus"
          variant="ghost"
          color="success"
          size="xs"
          class="h-8 w-8 justify-center p-0"
          :disabled="!newKey.trim() || !!newKeyError"
          aria-label="Add environment variable"
        />
      </form>
    </div>
  </div>
</template>
