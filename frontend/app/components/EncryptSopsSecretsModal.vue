<script setup lang="ts">
const props = defineProps<{ open: boolean }>()

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void
}>()

const { $pb } = useNuxtApp()
const { customPost } = useApi()
const { copy } = useCopy()
const toast = useToast()

const { data: repos } = useAsyncData('repos_for_sops_encrypt', () =>
  $pb.collection('repositories').getFullList({ sort: 'name', fields: 'id,name,sops_age_public_key' })
)

const repoOptions = computed(() =>
  (repos.value || []).map((r: any) => ({ label: r.name, value: r.id }))
)

const repositoryId = ref('')
type Row = { key: string, value: string }
const rows = ref<Row[]>([{ key: '', value: '' }])
const encrypting = ref(false)
const result = ref('')

const activeTab = ref('values')
const tabs = computed(() => [
  { label: 'Values', value: 'values', icon: 'i-lucide-list' },
  { label: 'Result', value: 'result', icon: 'i-lucide-file-lock-2', disabled: !result.value }
])

function addRow() {
  rows.value.push({ key: '', value: '' })
}

function removeRow(i: number) {
  rows.value.splice(i, 1)
  if (rows.value.length === 0) rows.value.push({ key: '', value: '' })
}

const validRows = computed(() => rows.value.filter(r => r.key.trim() !== ''))
const canEncrypt = computed(() => !!repositoryId.value && validRows.value.length > 0)

async function encrypt() {
  if (!canEncrypt.value) return
  encrypting.value = true
  result.value = ''
  try {
    const values: Record<string, string> = {}
    for (const row of validRows.value) values[row.key.trim()] = row.value
    const res = await customPost<{ content: string, filename: string }>(
      `/api/custom/repositories/${repositoryId.value}/sops-encrypt`,
      { values }
    )
    result.value = res.content
    activeTab.value = 'result'
  } catch (err: any) {
    toast.add({ title: 'Encryption failed', description: err.data?.error || err.message, color: 'error' })
  } finally {
    encrypting.value = false
  }
}

function downloadResult() {
  const blob = new Blob([result.value], { type: 'text/yaml' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = 'secrets.yaml'
  a.click()
  URL.revokeObjectURL(url)
}

function reset() {
  repositoryId.value = ''
  rows.value = [{ key: '', value: '' }]
  result.value = ''
  encrypting.value = false
  activeTab.value = 'values'
}

watch(() => props.open, (open) => {
  if (!open) reset()
})
</script>

<template>
  <UModal :open="open" :ui="{ content: 'w-full sm:max-w-lg' }" @update:open="v => emit('update:open', v)">
    <template #content>
      <UCard>
        <template #header>
          <div class="flex items-center gap-2">
            <UIcon name="i-lucide-file-lock-2" class="h-5 w-5 text-yellow-400" />
            <h3 class="font-semibold text-gray-900 dark:text-white">Encrypt Secrets for SOPS</h3>
          </div>
        </template>

        <UTabs v-model="activeTab" :items="tabs" class="mb-4" />

        <div v-if="activeTab === 'values'" class="space-y-4">
          <p class="text-xs text-gray-500">
            Values are encrypted with the chosen repository's age public key and returned as a
            <code class="font-mono">secrets.yaml</code>. Nothing is stored — copy or download the result and commit it
            yourself next to <code class="font-mono">wireops.yaml</code>.
          </p>

          <UFormField label="Repository" required>
            <AppSelectInput v-model="repositoryId" :items="repoOptions" :searchable="false" class="w-full" />
          </UFormField>

          <UFormField label="Secrets">
            <div class="space-y-2">
              <div v-for="(row, i) in rows" :key="i" class="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem] gap-2 items-center">
                <AppTextInput v-model="row.key" placeholder="KEY" class="font-mono" />
                <AppTextInput v-model="row.value" placeholder="value" type="password" class="font-mono" />
                <UButton
                  icon="i-lucide-x"
                  variant="ghost"
                  color="neutral"
                  size="xs"
                  class="h-8 w-8 justify-center p-0"
                  aria-label="Remove row"
                  @click="removeRow(i)"
                />
              </div>
              <UButton icon="i-lucide-plus" label="Add row" variant="outline" color="neutral" size="xs" @click="addRow" />
            </div>
          </UFormField>
        </div>

        <div v-else class="space-y-3">
          <div v-if="!result" class="text-center py-10 text-xs text-gray-500">
            Fill in the Values tab and click Encrypt to see the result here.
          </div>
          <template v-else>
            <p class="text-xs text-gray-500">Encrypted — copy or download and commit as <code class="font-mono">secrets.yaml</code>.</p>
            <pre class="max-h-64 overflow-auto rounded-md bg-gray-100 dark:bg-carbon-900 p-3 text-xs font-mono whitespace-pre-wrap break-all">{{ result }}</pre>
            <div class="flex gap-2">
              <UButton icon="i-lucide-copy" label="Copy" variant="outline" color="neutral" size="sm" @click="copy(result, 'secrets.yaml')" />
              <UButton icon="i-lucide-download" label="Download" variant="outline" color="neutral" size="sm" @click="downloadResult" />
            </div>
          </template>
        </div>

        <template #footer>
          <div class="flex justify-end gap-2">
            <UButton v-if="!result" label="Cancel" variant="outline" color="neutral" @click="emit('update:open', false)" />
            <UButton v-if="!result" label="Encrypt" icon="i-lucide-file-lock-2" color="primary" :loading="encrypting" :disabled="!canEncrypt" @click="encrypt" />
            <UButton v-else label="Close" variant="outline" color="neutral" @click="emit('update:open', false)" />
          </div>
        </template>
      </UCard>
    </template>
  </UModal>
</template>
