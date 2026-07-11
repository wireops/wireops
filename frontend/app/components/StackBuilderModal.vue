<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted } from 'vue'
import { buildStackYaml } from '../utils/stack-yaml-generator'
import { parseStackYaml } from '../utils/stack-yaml-parser'

interface StackBuilderWorker {
  id: string
  hostname: string
  status: string
  tags: string[]
}

const props = withDefaults(
  defineProps<{
    open?: boolean
    workers?: StackBuilderWorker[]
  }>(),
  {
    open: false,
    workers: () => [],
  }
)

const activeWorkers = computed(() => {
  return [...props.workers]
    .filter(w => w.status === 'ACTIVE')
    .sort((a, b) => a.hostname.localeCompare(b.hostname))
})

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void
}>()

const toast = useToast()

const form = ref({
  name: 'my-stack',
  removeOrphans: true,
  forcePull: false,
  waitRunning: false
})

const timeoutForm = ref({
  val: '',
  unit: 'm'
})

const syncForm = ref({
  val: '',
  unit: 's'
})

const timeoutUnits = [
  { label: 's (Sec)', value: 's' },
  { label: 'm (Min)', value: 'm' },
  { label: 'h (Hour)', value: 'h' }
]

const computedTimeout = computed(() => {
  const val = timeoutForm.value.val.trim()
  const num = Number(val)
  if (!val || Number.isNaN(num) || num === 0) return ''
  return val + timeoutForm.value.unit
})

const computedSyncInterval = computed(() => {
  const val = syncForm.value.val.trim()
  const num = Number(val)
  if (!val || Number.isNaN(num) || num === 0) return ''
  return val + syncForm.value.unit
})

const tagInput = ref('')
const tagsArray = ref<string[]>([])

function addTagsFromText(text: string) {
  if (!text) return
  const parts = text.split(/[\s,/]+/)
  parts.forEach(part => {
    const clean = part.trim()
    if (clean && !tagsArray.value.includes(clean)) {
      tagsArray.value.push(clean)
    }
  })
}

function handleTagInput() {
  const value = tagInput.value
  if (/[\s,/]/.test(value)) {
    addTagsFromText(value)
    tagInput.value = ''
  }
}

function handleTagBlur() {
  if (tagInput.value.trim()) {
    addTagsFromText(tagInput.value)
    tagInput.value = ''
  }
}

function handleTagBackspace() {
  if (tagInput.value === '' && tagsArray.value.length > 0) {
    tagsArray.value.pop()
  }
}

function handleTagEnter() {
  if (tagInput.value.trim()) {
    addTagsFromText(tagInput.value)
    tagInput.value = ''
  }
}

function removeTag(index: number) {
  tagsArray.value.splice(index, 1)
}

const yamlCode = computed(() => {
  return buildStackYaml({
    name: form.value.name,
    timeout: computedTimeout.value,
    removeOrphans: form.value.removeOrphans,
    forcePull: form.value.forcePull,
    waitRunning: form.value.waitRunning,
    workerTags: tagsArray.value,
    syncInterval: computedSyncInterval.value,
  })
})

const activeView = ref<'form' | 'yaml'>('form')
const isMobile = ref(false)

onMounted(() => {
  isMobile.value = window.innerWidth < 1024
  const resizeListener = () => { isMobile.value = window.innerWidth < 1024 }
  window.addEventListener('resize', resizeListener)
  onUnmounted(() => window.removeEventListener('resize', resizeListener))
})

function downloadYaml() {
  try {
    const blob = new Blob([yamlCode.value], { type: 'text/yaml' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'wireops.yaml'
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
    toast.add({ title: 'wireops.yaml downloaded', color: 'success' })
  } catch (err: any) {
    toast.add({ title: 'Failed to download yaml', description: err?.message, color: 'error' })
  }
}

async function copyYaml() {
  try {
    await navigator.clipboard.writeText(yamlCode.value)
    toast.add({ title: 'Copied to clipboard', color: 'success' })
  } catch (err: any) {
    toast.add({ title: 'Failed to copy', description: err?.message, color: 'error' })
  }
}

const isImportOpen = ref(false)
const importContent = ref('')

function handleImportYaml() {
  const content = importContent.value.trim()
  if (!content) {
    toast.add({ title: 'Please paste some YAML content', color: 'error' })
    return
  }

  try {
    const parsed = parseStackYaml(content)

    const hasMeaningfulContent = !!(
      parsed.name ||
      parsed.timeout ||
      parsed.removeOrphans !== undefined ||
      parsed.forcePull !== undefined ||
      parsed.waitRunning !== undefined ||
      parsed.syncInterval ||
      (parsed.workerTags && parsed.workerTags.length > 0)
    )

    if (!hasMeaningfulContent) {
      toast.add({ title: 'Invalid or incomplete wireops.yaml', description: 'The pasted YAML does not contain any valid stack definitions.', color: 'error' })
      return
    }

    if (parsed.name) form.value.name = parsed.name

    if (parsed.timeout) {
      const valMatch = parsed.timeout.match(/^[\d.]+/)
      if (valMatch) {
        timeoutForm.value.val = valMatch[0]
        const unit = parsed.timeout.replace(valMatch[0], '').trim()
        timeoutForm.value.unit = ['s', 'm', 'h'].includes(unit) ? unit : 'm'
      }
    } else {
      timeoutForm.value.val = ''
      timeoutForm.value.unit = 's'
    }

    if (parsed.removeOrphans !== undefined) form.value.removeOrphans = parsed.removeOrphans
    if (parsed.forcePull !== undefined) form.value.forcePull = parsed.forcePull
    if (parsed.waitRunning !== undefined) form.value.waitRunning = parsed.waitRunning

    if (parsed.workerTags) {
      tagsArray.value = [...parsed.workerTags]
    }

    if (parsed.syncInterval) {
      const valMatch = parsed.syncInterval.match(/^[\d.]+/)
      if (valMatch) {
        syncForm.value.val = valMatch[0]
        const unit = parsed.syncInterval.replace(valMatch[0], '').trim()
        syncForm.value.unit = ['s', 'm', 'h'].includes(unit) ? unit : 'm'
      }
    } else {
      syncForm.value.val = ''
      syncForm.value.unit = 's'
    }

    isImportOpen.value = false
    importContent.value = ''
    toast.add({ title: 'wireops.yaml imported successfully!', color: 'success' })
  } catch (err: any) {
    toast.add({ title: 'Failed to parse wireops.yaml', description: err?.message, color: 'error' })
  }
}
</script>

<template>
  <UModal
    :open="props.open"
    :fullscreen="isMobile"
    :ui="{ content: 'lg:max-w-5xl w-full' }"
    title="Stack Builder"
    description="Generate a wireops.yaml file for your Docker Compose stack"
    @update:open="emit('update:open', $event)"
  >
    <template #content>
      <UCard :ui="{ ring: '', divide: 'divide-y divide-gray-100 dark:divide-gray-800', base: 'h-full flex flex-col', body: { base: 'flex-1 overflow-y-auto p-0 sm:p-0' } }">
        <template #header>
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-2">
              <div class="flex items-center justify-center w-8 h-8 rounded bg-yellow-400/10">
                <UIcon name="i-lucide-wrench" class="w-4 h-4 text-yellow-400" />
              </div>
              <div>
                <h2 class="font-semibold text-lg text-gray-900 dark:text-wire-200">Stack Builder</h2>
                <p class="text-xs text-gray-500 dark:text-wire-200/50">Configure and generate a wireops.yaml for your stack</p>
              </div>
            </div>
            <UButton
              color="neutral"
              variant="ghost"
              icon="i-lucide-x"
              class="-my-1"
              aria-label="Close modal"
              @click="emit('update:open', false)"
            />
          </div>
        </template>

        <!-- Responsive Layout Tab Switcher (Mobile Only) -->
        <div class="lg:hidden flex border-b border-gray-200 dark:border-carbon-800">
          <button
            type="button"
            class="flex-1 py-3 text-center text-sm font-medium border-b-2 transition-colors"
            :class="activeView === 'form' ? 'border-yellow-400 text-yellow-500' : 'border-transparent text-gray-500 dark:text-wire-200/50'"
            @click="activeView = 'form'"
          >
            <span class="flex items-center justify-center gap-2">
              <UIcon name="i-lucide-settings" class="w-4 h-4" />
              Configuration Form
            </span>
          </button>
          <button
            type="button"
            class="flex-1 py-3 text-center text-sm font-medium border-b-2 transition-colors"
            :class="activeView === 'yaml' ? 'border-yellow-400 text-yellow-500' : 'border-transparent text-gray-500 dark:text-wire-200/50'"
            @click="activeView = 'yaml'"
          >
            <span class="flex items-center justify-center gap-2">
              <UIcon name="i-lucide-terminal" class="w-4 h-4" />
              YAML Preview
            </span>
          </button>
        </div>

        <div class="grid grid-cols-1 lg:grid-cols-2 divide-y lg:divide-y-0 lg:divide-x divide-gray-200 dark:divide-carbon-800 min-h-[500px]">
          <!-- LEFT COLUMN: Form -->
          <div
            v-show="!isMobile || activeView === 'form'"
            class="p-3 sm:p-4 space-y-3 max-h-[70vh] overflow-y-auto"
          >
            <!-- Basic Info Card -->
            <div class="space-y-3 border border-gray-200 dark:border-carbon-800/60 rounded-lg p-3 bg-gray-50/50 dark:bg-carbon-900/10">
              <div class="flex items-center gap-1.5 border-b border-gray-150 dark:border-carbon-800/30 pb-1.5 mb-1">
                <UIcon name="i-lucide-info" class="w-4 h-4 text-yellow-400 shrink-0" />
                <span class="text-xs uppercase tracking-wider font-bold text-gray-500 dark:text-wire-200/50">Basic Info</span>
              </div>
              <UFormField label="Name" required class="w-full">
                <AppTextInput v-model="form.name" placeholder="e.g. production-api" aria-label="Stack name" />
              </UFormField>
            </div>

            <!-- Sync Card -->
            <div class="space-y-3 border border-gray-200 dark:border-carbon-800/60 rounded-lg p-3 bg-gray-50/50 dark:bg-carbon-900/10">
              <div class="flex items-center gap-1.5 border-b border-gray-150 dark:border-carbon-800/30 pb-1.5 mb-1">
                <UIcon name="i-lucide-refresh-cw" class="w-4 h-4 text-yellow-400 shrink-0" />
                <span class="text-xs uppercase tracking-wider font-bold text-gray-500 dark:text-wire-200/50">Sync</span>
              </div>

              <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <UFormField class="w-full">
                  <template #label>
                    <span class="flex items-center gap-1">
                      Deploy Timeout
                      <UTooltip>
                        <UIcon name="i-lucide-help-circle" class="w-3.5 h-3.5 text-gray-400 cursor-help" />
                        <template #content>
                          <div class="text-xs leading-normal max-w-[220px]">
                            Max time allowed for a deploy to complete before it's considered failed. Leave empty to use the global default (5m).
                          </div>
                        </template>
                      </UTooltip>
                    </span>
                  </template>
                  <div class="flex gap-2 items-center w-full">
                    <AppTextInput v-model="timeoutForm.val" placeholder="5" aria-label="Deploy timeout value" class="flex-1 min-w-0" />
                    <USelect v-model="timeoutForm.unit" :items="timeoutUnits" size="sm" class="w-[90px] shrink-0" />
                  </div>
                </UFormField>

                <UFormField class="w-full">
                  <template #label>
                    <span class="flex items-center gap-1">
                      Poll Interval
                      <UTooltip>
                        <UIcon name="i-lucide-help-circle" class="w-3.5 h-3.5 text-gray-400 cursor-help" />
                        <template #content>
                          <div class="text-xs leading-normal max-w-[220px]">
                            How often the repository is polled for changes. Leave empty to use the global default (10s).
                          </div>
                        </template>
                      </UTooltip>
                    </span>
                  </template>
                  <div class="flex gap-2 items-center w-full">
                    <AppTextInput v-model="syncForm.val" placeholder="10" aria-label="Poll interval value" class="flex-1 min-w-0" />
                    <USelect v-model="syncForm.unit" :items="timeoutUnits" size="sm" class="w-[90px] shrink-0" />
                  </div>
                </UFormField>
              </div>
            </div>

            <!-- Compose Config Card -->
            <div class="space-y-3 border border-gray-200 dark:border-carbon-800/60 rounded-lg p-3 bg-gray-50/50 dark:bg-carbon-900/10">
              <div class="flex items-center gap-1.5 border-b border-gray-150 dark:border-carbon-800/30 pb-1.5 mb-1">
                <UIcon name="i-lucide-boxes" class="w-4 h-4 text-yellow-400 shrink-0" />
                <span class="text-xs uppercase tracking-wider font-bold text-gray-500 dark:text-wire-200/50">Compose Config</span>
              </div>

              <label class="flex items-center justify-between gap-2 cursor-pointer">
                <span class="text-sm text-gray-700 dark:text-wire-200 flex items-center gap-1">
                  Remove Orphans
                  <UTooltip>
                    <UIcon name="i-lucide-help-circle" class="w-3.5 h-3.5 text-gray-400 cursor-help" />
                    <template #content>
                      <div class="text-xs leading-normal max-w-[220px]">
                        Removes containers for services no longer defined in the compose file on each deploy.
                      </div>
                    </template>
                  </UTooltip>
                </span>
                <USwitch v-model="form.removeOrphans" />
              </label>

              <label class="flex items-center justify-between gap-2 cursor-pointer">
                <span class="text-sm text-gray-700 dark:text-wire-200 flex items-center gap-1">
                  Force Pull
                  <UTooltip>
                    <UIcon name="i-lucide-help-circle" class="w-3.5 h-3.5 text-gray-400 cursor-help" />
                    <template #content>
                      <div class="text-xs leading-normal max-w-[220px]">
                        Always pulls fresh images before deploy, even if unchanged.
                      </div>
                    </template>
                  </UTooltip>
                </span>
                <USwitch v-model="form.forcePull" />
              </label>
            </div>

            <!-- Jobs Config Card -->
            <div class="space-y-3 border border-gray-200 dark:border-carbon-800/60 rounded-lg p-3 bg-gray-50/50 dark:bg-carbon-900/10">
              <div class="flex items-center gap-1.5 border-b border-gray-150 dark:border-carbon-800/30 pb-1.5 mb-1">
                <UIcon name="i-lucide-clock" class="w-4 h-4 text-yellow-400 shrink-0" />
                <span class="text-xs uppercase tracking-wider font-bold text-gray-500 dark:text-wire-200/50">Jobs Config</span>
              </div>

              <label class="flex items-center justify-between gap-2 cursor-pointer">
                <span class="text-sm text-gray-700 dark:text-wire-200 flex items-center gap-1">
                  Wait for Running Jobs
                  <UTooltip>
                    <UIcon name="i-lucide-help-circle" class="w-3.5 h-3.5 text-gray-400 cursor-help" />
                    <template #content>
                      <div class="text-xs leading-normal max-w-[220px]">
                        Deploy waits for any related jobs currently running to finish first.
                      </div>
                    </template>
                  </UTooltip>
                </span>
                <USwitch v-model="form.waitRunning" />
              </label>
            </div>

            <!-- Worker Config Card -->
            <div class="space-y-3 border border-gray-200 dark:border-carbon-800/60 rounded-lg p-3 bg-gray-50/50 dark:bg-carbon-900/10">
              <div class="flex items-center gap-1.5 border-b border-gray-150 dark:border-carbon-800/30 pb-1.5 mb-1">
                <UIcon name="i-lucide-server" class="w-4 h-4 text-yellow-400 shrink-0" />
                <span class="text-xs uppercase tracking-wider font-bold text-gray-500 dark:text-wire-200/50">Worker Config</span>
              </div>

              <UFormField class="w-full">
                <template #label>
                  <span class="flex items-center gap-2">
                    Filter Tags
                    <span class="text-xs font-normal text-gray-400 dark:text-wire-200/40">(Separated by space, comma or /)</span>
                  </span>
                </template>
                <div class="flex flex-wrap gap-1.5 p-1.5 border border-gray-200 dark:border-carbon-800 rounded-lg bg-white dark:bg-carbon-950/20 focus-within:border-yellow-400/60 focus-within:ring-1 focus-within:ring-yellow-400/40 transition-all duration-200 w-full min-h-[38px] items-center">
                  <div
                    v-for="(tag, idx) in tagsArray"
                    :key="idx"
                    class="bg-yellow-400/10 text-yellow-600 dark:text-yellow-400 text-xs px-2 py-0.5 rounded flex items-center gap-1 font-semibold border border-yellow-400/20"
                  >
                    <span>{{ tag }}</span>
                    <UButton
                      icon="i-lucide-x"
                      size="xs"
                      variant="ghost"
                      class="p-0 h-3.5 w-3.5 hover:bg-transparent -my-0.5 text-yellow-600 dark:text-yellow-400 opacity-60 hover:opacity-100 transition-opacity"
                      @click="removeTag(idx)"
                    />
                  </div>
                  <input
                    id="stack-worker-tags-input"
                    v-model="tagInput"
                    type="text"
                    class="flex-1 min-w-[120px] bg-transparent border-0 p-0 focus:ring-0 focus:outline-hidden text-sm h-6 text-gray-900 dark:text-white placeholder-gray-400 dark:placeholder-wire-200/30"
                    placeholder="Add tag..."
                    aria-label="Add worker tag"
                    @input="handleTagInput"
                    @blur="handleTagBlur"
                    @keydown.backspace="handleTagBackspace"
                    @keydown.enter.prevent="handleTagEnter"
                  >
                </div>
              </UFormField>

              <div class="space-y-1.5">
                <span class="text-xs font-semibold text-gray-500 dark:text-wire-200/50">Known Workers</span>
                <p v-if="activeWorkers.length === 0" class="text-xs text-gray-500 dark:text-wire-200/40 italic py-2 text-center border border-dashed border-gray-200 dark:border-carbon-800 rounded-lg bg-white/20 dark:bg-carbon-900/5">
                  No active workers found.
                </p>
                <div v-else class="space-y-1.5">
                  <div
                    v-for="worker in activeWorkers"
                    :key="worker.id"
                    class="flex flex-wrap items-center gap-1.5 border border-gray-200/60 dark:border-carbon-800/40 rounded bg-white dark:bg-carbon-900/40 px-2 py-1.5"
                  >
                    <span class="text-xs font-medium text-gray-700 dark:text-wire-200 shrink-0">{{ worker.hostname }}</span>
                    <template v-if="worker.tags.length">
                      <UButton
                        v-for="tag in worker.tags"
                        :key="tag"
                        :label="tag"
                        size="xs"
                        color="primary"
                        variant="outline"
                        class="rounded-full"
                        @click="addTagsFromText(tag)"
                      />
                    </template>
                    <span v-else class="text-xs text-gray-400 dark:text-wire-200/30 italic">no tags</span>
                  </div>
                </div>
              </div>
            </div>
          </div>

          <!-- RIGHT COLUMN: YAML Preview -->
          <div
            v-show="!isMobile || activeView === 'yaml'"
            class="bg-gray-50 dark:bg-carbon-900/10 p-4 sm:p-6 flex flex-col justify-between max-h-[70vh]"
          >
            <div class="flex-1 flex flex-col min-h-0">
              <div class="flex items-center justify-between mb-3 shrink-0">
                <div class="flex items-center gap-2">
                  <UIcon name="i-lucide-terminal" class="w-4 h-4 text-yellow-400" />
                  <span class="font-semibold text-sm text-gray-900 dark:text-wire-200"><span class="opacity-50">Preview</span> wireops.yaml</span>
                </div>

                <div class="flex items-center gap-2">
                  <UButton
                    v-if="!isImportOpen"
                    icon="i-lucide-upload"
                    label="Import"
                    size="xs"
                    color="neutral"
                    variant="soft"
                    @click="isImportOpen = true"
                  />
                  <UButton
                    icon="i-lucide-copy"
                    label="Copy"
                    size="xs"
                    color="neutral"
                    variant="soft"
                    @click="copyYaml"
                  />
                  <UButton
                    icon="i-lucide-download"
                    label="Download"
                    size="xs"
                    color="primary"
                    variant="solid"
                    @click="downloadYaml"
                  />
                </div>
              </div>

              <!-- Scrollable code highlighter wrapper / Import textarea -->
              <div class="flex-1 overflow-hidden rounded-lg border border-gray-200 dark:border-carbon-800 flex flex-col bg-white dark:bg-carbon-950">
                <div v-if="isImportOpen" class="flex-1 flex flex-col p-3 gap-3">
                  <span class="text-xs font-semibold text-gray-500 dark:text-wire-200/60 uppercase tracking-wider">Paste your wireops.yaml content:</span>
                  <textarea
                    id="stack-yaml-import-textarea"
                    v-model="importContent"
                    class="flex-1 p-2.5 font-mono text-xs bg-gray-50 dark:bg-carbon-900 border border-gray-200 dark:border-carbon-800 rounded-md text-gray-900 dark:text-white focus:outline-hidden focus:ring-1 focus:ring-yellow-400/50 resize-none min-h-[250px]"
                    placeholder="version: wireops.v1&#10;name: my-stack&#10;..."
                    aria-label="Paste your wireops.yaml content"
                  />
                  <div class="flex justify-end gap-2 shrink-0">
                    <UButton
                      label="Cancel"
                      size="xs"
                      color="neutral"
                      variant="outline"
                      @click="isImportOpen = false; importContent = ''"
                    />
                    <UButton
                      label="Load YAML"
                      size="xs"
                      color="primary"
                      variant="solid"
                      @click="handleImportYaml"
                    />
                  </div>
                </div>
                <YamlHighlighter v-else :code="yamlCode" class="h-full overflow-y-auto" />
              </div>
            </div>

            <div class="mt-4 pt-4 border-t border-gray-200 dark:border-carbon-800/60 text-xs text-gray-500 dark:text-wire-200/50 shrink-0">
              <p class="flex items-start gap-2">
                <UIcon name="i-lucide-info" class="w-4 h-4 text-info-500 shrink-0 mt-0.5" />
                <span>Save this file as <code class="bg-gray-100 dark:bg-carbon-800 px-1 py-0.5 rounded text-xs text-yellow-500">wireops.yaml</code> next to your compose file in your Git repository, then create a stack pointing to it.</span>
              </p>
            </div>
          </div>
        </div>

        <template #footer>
          <div class="flex justify-end gap-2">
            <UButton label="Close" color="neutral" variant="outline" @click="emit('update:open', false)" />
          </div>
        </template>
      </UCard>
    </template>
  </UModal>
</template>
