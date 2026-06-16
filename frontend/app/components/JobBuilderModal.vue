<script setup lang="ts">
import { computed, ref, watch, onMounted, onUnmounted } from 'vue'

const props = withDefaults(
  defineProps<{
    open?: boolean
  }>(),
  {
    open: false,
  }
)

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void
}>()

const toast = useToast()

const form = ref({
  name: 'my-scheduled-job',
  description: 'A brief description of what this job does',
  cron: '*/5 * * * *',
  mode: 'once',
  image: 'ubuntu:latest',
  command: 'echo "hello from wireops"',
  commandAsArray: false,
  network: '',
  tags: 'production, cleanup'
})

const resourceForm = ref({
  cpuVal: '0.5',
  cpuUnit: 'cores',
  memoryVal: '512',
  memoryUnit: 'm',
  timeoutVal: '5',
  timeoutUnit: 'm'
})

const cpuUnits = [
  { label: 'Cores', value: 'cores' },
  { label: 'm (mCores)', value: 'm' }
]

const memoryUnits = [
  { label: 'MB', value: 'm' },
  { label: 'GB', value: 'g' }
]

const timeoutUnits = [
  { label: 's (Sec)', value: 's' },
  { label: 'm (Min)', value: 'm' },
  { label: 'h (Hour)', value: 'h' }
]

const computedCpu = computed(() => {
  const val = resourceForm.value.cpuVal.trim()
  if (!val) return ''
  const unit = resourceForm.value.cpuUnit === 'cores' ? '' : resourceForm.value.cpuUnit
  return val + unit
})

const computedMemory = computed(() => {
  const val = resourceForm.value.memoryVal.trim()
  if (!val) return ''
  return val + resourceForm.value.memoryUnit
})

const computedTimeout = computed(() => {
  const val = resourceForm.value.timeoutVal.trim()
  if (!val) return ''
  return val + resourceForm.value.timeoutUnit
})

const cronTab = ref<'presets' | 'custom'>('presets')
const selectedPreset = ref('*/5 * * * *')

const cronPresets = [
  { label: 'Every minute (* * * * *)', value: '* * * * *' },
  { label: 'Every 5 minutes (*/5 * * * *)', value: '*/5 * * * *' },
  { label: 'Every 15 minutes (*/15 * * * *)', value: '*/15 * * * *' },
  { label: 'Every 30 minutes (*/30 * * * *)', value: '*/30 * * * *' },
  { label: 'Every hour (0 * * * *)', value: '0 * * * *' },
  { label: 'Daily at Midnight (0 0 * * *)', value: '0 0 * * *' },
  { label: 'Weekly on Sunday (0 0 * * 0)', value: '0 0 * * 0' },
  { label: 'Monthly on 1st (0 0 1 * *)', value: '0 0 1 * *' }
]

watch([cronTab, selectedPreset], () => {
  if (cronTab.value === 'presets') {
    form.value.cron = selectedPreset.value
  }
}, { immediate: true })

const volumes = ref<{ host: string; container: string }[]>([
  { host: '/var/log', container: '/app/logs' }
])

function addVolume() {
  volumes.value.push({ host: '', container: '' })
}

function removeVolume(index: number) {
  volumes.value.splice(index, 1)
}

const volumesList = computed(() => {
  return volumes.value
    .map(v => ({ host: v.host.trim(), container: v.container.trim() }))
    .filter(v => v.host && v.container)
})

const tagInput = ref('')
const tagsArray = ref<string[]>(['production', 'cleanup'])

watch(tagsArray, (newVal) => {
  form.value.tags = newVal.join(', ')
}, { deep: true, immediate: true })

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

const tagsList = computed(() => {
  return tagsArray.value
})

// Build clean, well-formatted YAML
const yamlCode = computed(() => {
  const lines: string[] = []
  
  lines.push(`name: "${form.value.name.replace(/"/g, '\\"')}"`)
  lines.push(`description: "${form.value.description.replace(/"/g, '\\"')}"`)
  lines.push(`cron: "${form.value.cron.replace(/"/g, '\\"')}"`)
  
  if (tagsList.value.length > 0) {
    lines.push('tags:')
    tagsList.value.forEach(tag => {
      lines.push(`  - "${tag.replace(/"/g, '\\"')}"`)
    })
  } else {
    lines.push('tags: []')
  }

  lines.push(`mode: "${form.value.mode}"`)
  lines.push(`image: "${form.value.image.replace(/"/g, '\\"')}"`)
  
  const cmdTrimmed = form.value.command.trim()
  if (cmdTrimmed || isCommandFocused.value) {
    if (form.value.commandAsArray) {
      const parts = parseCommand(cmdTrimmed)
      lines.push(`command: [${parts.map(p => `"${p.replace(/"/g, '\\"')}"`).join(', ')}]`)
    } else {
      lines.push(`command: "${cmdTrimmed.replace(/"/g, '\\"')}"`)
    }
  }

  if (volumesList.value.length > 0) {
    lines.push('volumes:')
    volumesList.value.forEach(vol => {
      const volStr = `${vol.host}:${vol.container}`
      lines.push(`  - "${volStr.replace(/"/g, '\\"')}"`)
    })
  } else {
    lines.push('volumes: []')
  }

  if (form.value.network.trim()) {
    lines.push(`network: "${form.value.network.trim().replace(/"/g, '\\"')}"`)
  }

  lines.push('resources:')
  lines.push(`  cpu: "${computedCpu.value.replace(/"/g, '\\"')}"`)
  lines.push(`  memory: "${computedMemory.value.replace(/"/g, '\\"')}"`)
  lines.push(`  timeout: "${computedTimeout.value.replace(/"/g, '\\"')}"`)

  return lines.join('\n')
})

const modeOptions = [
  { label: 'Once', value: 'once' },
  { label: 'Once All', value: 'once_all' }
]

const activeView = ref<'form' | 'yaml'>('form')
const isCommandFocused = ref(false)
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
    a.download = 'job.yaml'
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
    toast.add({ title: 'job.yaml downloaded', color: 'success' })
  } catch (err: any) {
    toast.add({ title: 'Failed to download yaml', description: err?.message, color: 'error' })
  }
}

function parseCommand(cmd: string): string[] {
  const args: string[] = []
  const regex = /[^\s"']+|"([^"]*)"|'([^']*)'/g
  let match
  while ((match = regex.exec(cmd)) !== null) {
    if (match[1] !== undefined) {
      args.push(match[1])
    } else if (match[2] !== undefined) {
      args.push(match[2])
    } else {
      const clean = match[0].replace(/^,+|,+$/g, '')
      if (clean) {
        args.push(clean)
      }
    }
  }
  return args
}

async function copyYaml() {
  try {
    await navigator.clipboard.writeText(yamlCode.value)
    toast.add({ title: 'Copied to clipboard', color: 'success' })
  } catch (err: any) {
    toast.add({ title: 'Failed to copy', description: err?.message, color: 'error' })
  }
}

const cronExplanation = computed(() => {
  return translateCron(form.value.cron)
})

const volumeWarnings = computed(() => {
  const warnings: Record<number, string> = {}
  volumes.value.forEach((vol, idx) => {
    const host = vol.host.trim()
    if (host.includes('/') && !host.startsWith('/')) {
      warnings[idx] = 'Host path must be an absolute path (starting with "/")'
    } else if (host.includes('\\')) {
      warnings[idx] = 'Use forward slashes "/" for paths, not backslashes'
    }
  })
  return warnings
})

const isImportOpen = ref(false)
const importContent = ref('')

function handleImportYaml() {
  const content = importContent.value.trim()
  if (!content) {
    toast.add({ title: 'Please paste some YAML content', color: 'error' })
    return
  }

  try {
    const parsed = parseJobYaml(content)
    
    if (parsed.name) form.value.name = parsed.name
    if (parsed.description) form.value.description = parsed.description
    if (parsed.cron) {
      form.value.cron = parsed.cron
      const presetMatch = cronPresets.find(p => p.value === parsed.cron)
      if (presetMatch) {
        cronTab.value = 'presets'
        selectedPreset.value = parsed.cron
      } else {
        cronTab.value = 'custom'
      }
    }
    if (parsed.mode) form.value.mode = parsed.mode
    if (parsed.image) form.value.image = parsed.image
    if (parsed.command) {
      form.value.command = parsed.command
      form.value.commandAsArray = !!parsed.commandAsArray
    }
    if (parsed.network !== undefined) form.value.network = parsed.network

    if (parsed.tags) {
      tagsArray.value = [...parsed.tags]
    }

    if (parsed.volumes) {
      volumes.value = parsed.volumes.map(v => ({ host: v.host, container: v.container }))
    }

    if (parsed.cpu) {
      const cpuValMatch = parsed.cpu.match(/^[\d.]+/)
      if (cpuValMatch) {
        resourceForm.value.cpuVal = cpuValMatch[0]
        const unit = parsed.cpu.replace(cpuValMatch[0], '').trim()
        resourceForm.value.cpuUnit = unit === 'm' ? 'm' : 'cores'
      }
    }
    if (parsed.memory) {
      const memValMatch = parsed.memory.match(/^[\d.]+/)
      if (memValMatch) {
        resourceForm.value.memoryVal = memValMatch[0]
        const unit = parsed.memory.replace(memValMatch[0], '').trim()
        resourceForm.value.memoryUnit = unit === 'g' ? 'g' : 'm'
      }
    }
    if (parsed.timeout) {
      const timeoutValMatch = parsed.timeout.match(/^[\d.]+/)
      if (timeoutValMatch) {
        resourceForm.value.timeoutVal = timeoutValMatch[0]
        const unit = parsed.timeout.replace(timeoutValMatch[0], '').trim()
        resourceForm.value.timeoutUnit = ['s', 'm', 'h'].includes(unit) ? unit : 'm'
      }
    }

    isImportOpen.value = false
    importContent.value = ''
    toast.add({ title: 'job.yaml imported successfully!', color: 'success' })
  } catch (err: any) {
    toast.add({ title: 'Failed to parse job.yaml', description: err?.message, color: 'error' })
  }
}
</script>

<template>
  <UModal
    :open="props.open"
    :fullscreen="isMobile"
    :ui="{ content: 'lg:max-w-5xl w-full' }"
    title="Job Builder"
    description="Generate a job.yaml file for scheduled Docker jobs"
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
                <h2 class="font-semibold text-lg text-gray-900 dark:text-wire-200">Job Builder</h2>
                <p class="text-xs text-gray-500 dark:text-wire-200/50">Configure and generate a job.yaml for your scheduled jobs</p>
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
          <!-- LEFT COLUMN: Form (visible on lg, or on mobile when activeView === 'form') -->
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
                <UInput v-model="form.name" placeholder="e.g. database-backup" size="sm" class="w-full" />
              </UFormField>
              <UFormField label="Description" required class="w-full">
                <UInput v-model="form.description" placeholder="e.g. Periodically backup production db" size="sm" class="w-full" />
              </UFormField>
            </div>

            <!-- Execution Config Card -->
            <div class="space-y-3 border border-gray-200 dark:border-carbon-800/60 rounded-lg p-3 bg-gray-50/50 dark:bg-carbon-900/10">
              <div class="flex items-center gap-1.5 border-b border-gray-150 dark:border-carbon-800/30 pb-1.5 mb-1">
                <UIcon name="i-lucide-settings" class="w-4 h-4 text-yellow-400 shrink-0" />
                <span class="text-xs uppercase tracking-wider font-bold text-gray-500 dark:text-wire-200/50">Execution Config</span>
              </div>

              <!-- Docker Image (First item, full width) -->
              <UFormField label="Docker Image" required class="w-full">
                <UInput v-model="form.image" placeholder="e.g. postgres:15-alpine" size="sm" class="w-full" />
              </UFormField>

              <!-- Cron Tabbed Selector -->
              <div class="space-y-2 border border-gray-200/60 dark:border-carbon-800/40 rounded bg-white dark:bg-carbon-900/40 p-2.5">
                <div class="flex items-center justify-between">
                  <span class="text-xs font-semibold text-gray-700 dark:text-wire-200 flex items-center gap-1">
                    <UIcon name="i-lucide-calendar-clock" class="w-4 h-4 text-yellow-400" />
                    Cron Schedule <span class="text-red-500">*</span>
                  </span>
                  <div class="flex border border-gray-200 dark:border-carbon-800 rounded bg-gray-100 dark:bg-carbon-900 p-0.5 text-[10px]">
                    <button
                      type="button"
                      class="px-2 py-0.5 rounded transition-colors font-medium"
                      :class="cronTab === 'presets' ? 'bg-yellow-400 text-gray-950 shadow-sm' : 'text-gray-500 dark:text-wire-200/50 hover:text-gray-700 dark:hover:text-wire-200'"
                      @click="cronTab = 'presets'"
                    >
                      Presets
                    </button>
                    <button
                      type="button"
                      class="px-2 py-0.5 rounded transition-colors font-medium"
                      :class="cronTab === 'custom' ? 'bg-yellow-400 text-gray-950 shadow-sm' : 'text-gray-500 dark:text-wire-200/50 hover:text-gray-700 dark:hover:text-wire-200'"
                      @click="cronTab = 'custom'"
                    >
                      Custom
                    </button>
                  </div>
                </div>

                <div v-show="cronTab === 'presets'">
                  <USelect
                    v-model="selectedPreset"
                    :items="cronPresets"
                    placeholder="Select frequency preset"
                    size="sm"
                    class="w-full"
                  />
                </div>
                <div v-show="cronTab === 'custom'">
                  <UInput
                    v-model="form.cron"
                    placeholder="e.g. */5 * * * *"
                    size="sm"
                    class="w-full"
                  />
                </div>
                <!-- Cron Translation Explanation -->
                <div class="mt-1.5 flex items-center gap-1.5 px-1 text-[11px] font-mono text-gray-500 dark:text-wire-200/50">
                  <UIcon name="i-lucide-clock" class="w-3.5 h-3.5 text-yellow-500/80 shrink-0" />
                  <span class="truncate">{{ cronExplanation }}</span>
                </div>
              </div>

              <!-- Mode (full width, with ? icon and tooltip) -->
              <UFormField class="w-full">
                <template #label>
                  <span class="flex items-center gap-1">
                    Mode <span class="text-red-500">*</span>
                    <UTooltip>
                      <UIcon name="i-lucide-help-circle" class="w-3.5 h-3.5 text-gray-400 cursor-help" />
                      <template #content>
                        <div class="space-y-1 text-xs leading-normal">
                          <p><strong class="text-yellow-400">once:</strong> dispatches to a single worker (round-robin).</p>
                          <p><strong class="text-yellow-400">once_all:</strong> dispatches to all matching workers concurrently.</p>
                        </div>
                      </template>
                    </UTooltip>
                  </span>
                </template>
                <USelect v-model="form.mode" :items="modeOptions" placeholder="Select mode" size="sm" class="w-full" />
              </UFormField>

              <UFormField label="Command" class="w-full" :ui="{ label: 'w-full block' }">
                <template #label>
                  <div class="flex items-center justify-between w-full">
                    <span>Command</span>
                    <label class="flex items-center gap-1.5 text-xs font-normal text-gray-500 dark:text-wire-200/60 cursor-pointer">
                      <input v-model="form.commandAsArray" type="checkbox" class="rounded border-gray-300 dark:border-carbon-700 text-yellow-500 focus:ring-yellow-400">
                      Format as Array
                    </label>
                  </div>
                </template>
                <div 
                  class="border rounded-lg overflow-hidden shadow-xs bg-carbon-950 text-wire-200 transition-all duration-200 w-full"
                  :class="isCommandFocused ? 'border-yellow-400/60 ring-1 ring-yellow-400/40' : 'border-gray-200 dark:border-carbon-800'"
                >
                  <!-- Terminal Body -->
                  <div class="p-2.5 font-mono text-sm flex gap-2 items-center bg-carbon-950">
                    <span class="text-green-400 font-bold select-none">$</span>
                    <input
                      id="terminal-command-input"
                      v-model="form.command"
                      type="text"
                      class="flex-1 bg-transparent border-0 p-0 text-white placeholder-gray-600 focus:ring-0 focus:outline-hidden text-sm font-mono"
                      placeholder="e.g. pg_dump -U postgres dbname"
                      aria-label="Command"
                      @focus="isCommandFocused = true"
                      @blur="isCommandFocused = false"
                    >
                  </div>
                </div>
              </UFormField>

              <UFormField label="Tags" class="w-full">
                <template #label>
                  <span class="flex items-center gap-2">
                    Tags
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
                    v-model="tagInput"
                    type="text"
                    class="flex-1 min-w-[120px] bg-transparent border-0 p-0 focus:ring-0 focus:outline-hidden text-sm h-6 text-gray-900 dark:text-white placeholder-gray-400 dark:placeholder-wire-200/30"
                    placeholder="Add tag..."
                    @input="handleTagInput"
                    @blur="handleTagBlur"
                    @keydown.backspace="handleTagBackspace"
                    @keydown.enter.prevent="handleTagEnter"
                  >
                </div>
              </UFormField>
            </div>

            <!-- Volumes & Network Card -->
            <div class="space-y-3 border border-gray-200 dark:border-carbon-800/60 rounded-lg p-3 bg-gray-50/50 dark:bg-carbon-900/10">
              <div class="flex items-center justify-between border-b border-gray-150 dark:border-carbon-800/30 pb-1.5 mb-1">
                <div class="flex items-center gap-1.5">
                  <UIcon name="i-lucide-layers" class="w-4 h-4 text-yellow-400 shrink-0" />
                  <span class="text-xs uppercase tracking-wider font-bold text-gray-500 dark:text-wire-200/50">Volumes & Network</span>
                </div>
                <UButton
                  icon="i-lucide-plus"
                  label="Add Volume"
                  size="xs"
                  variant="ghost"
                  color="neutral"
                  class="-my-1"
                  @click="addVolume"
                />
              </div>

              <div class="space-y-2.5">
                <div 
                  v-for="(vol, idx) in volumes" 
                  :key="idx" 
                  class="group border border-gray-200 dark:border-carbon-800 rounded-lg p-2.5 bg-white dark:bg-carbon-950/20 shadow-xs relative hover:border-yellow-400/50 dark:hover:border-yellow-400/30 transition-all duration-200"
                >
                  <div class="flex items-center justify-between mb-2">
                    <span class="text-[10px] uppercase font-bold tracking-wider text-yellow-600 dark:text-yellow-400 flex items-center gap-1">
                      <UIcon name="i-lucide-hard-drive" class="w-3.5 h-3.5" />
                      Mapping #{{ idx + 1 }}
                    </span>
                    <UButton
                      icon="i-lucide-trash"
                      color="error"
                      variant="ghost"
                      size="xs"
                      class="opacity-70 hover:opacity-100 transition-opacity -my-1"
                      @click="removeVolume(idx)"
                    />
                  </div>
                  
                  <div class="grid grid-cols-1 sm:grid-cols-[1fr_auto_1fr] gap-2 items-center">
                    <div class="space-y-1">
                      <label class="text-[10px] font-semibold text-gray-500 dark:text-wire-200/40 uppercase tracking-wide">
                        Host Path
                      </label>
                      <UInput v-model="vol.host" placeholder="e.g. /var/log" size="sm" class="w-full font-mono text-xs" />
                      <p v-if="volumeWarnings[idx]" class="text-[10px] text-red-500 dark:text-red-400 mt-0.5 font-sans flex items-center gap-1">
                        <UIcon name="i-lucide-alert-triangle" class="w-3 h-3 shrink-0" />
                        {{ volumeWarnings[idx] }}
                      </p>
                    </div>
                    <div class="hidden sm:block text-gray-400 dark:text-wire-200/20 font-bold font-mono self-end pb-2.5">:</div>
                    <div class="space-y-1">
                      <label class="text-[10px] font-semibold text-gray-500 dark:text-wire-200/40 uppercase tracking-wide">
                        Container Path
                      </label>
                      <UInput v-model="vol.container" placeholder="e.g. /app/logs" size="sm" class="w-full font-mono text-xs" />
                    </div>
                  </div>
                </div>
                
                <p v-if="volumes.length === 0" class="text-xs text-gray-500 dark:text-wire-200/40 italic py-3 text-center border border-dashed border-gray-200 dark:border-carbon-800 rounded-lg bg-white/20 dark:bg-carbon-900/5">
                  No volumes configured. Click "Add Volume" to map host/container paths.
                </p>
              </div>

              <UFormField label="Network (Optional)" class="w-full">
                <UInput v-model="form.network" placeholder="e.g. my-docker-network" size="sm" class="w-full" />
              </UFormField>
            </div>

            <!-- Resources Card -->
            <div class="space-y-3 border border-gray-200 dark:border-carbon-800/60 rounded-lg p-3 bg-gray-50/50 dark:bg-carbon-900/10">
              <div class="flex items-center gap-1.5 border-b border-gray-150 dark:border-carbon-800/30 pb-1.5 mb-1">
                <UIcon name="i-lucide-cpu" class="w-4 h-4 text-yellow-400 shrink-0" />
                <span class="text-xs uppercase tracking-wider font-bold text-gray-500 dark:text-wire-200/50">Resources</span>
              </div>

              <div class="space-y-3">
                <UFormField label="CPU" required>
                  <div class="flex gap-2 items-center w-full">
                    <UInput v-model="resourceForm.cpuVal" placeholder="0.5" size="sm" class="flex-1 min-w-0" />
                    <USelect v-model="resourceForm.cpuUnit" :items="cpuUnits" size="sm" class="w-[110px] shrink-0" />
                  </div>
                </UFormField>
                <UFormField label="Memory" required>
                  <div class="flex gap-2 items-center w-full">
                    <UInput v-model="resourceForm.memoryVal" placeholder="512" size="sm" class="flex-1 min-w-0" />
                    <USelect v-model="resourceForm.memoryUnit" :items="memoryUnits" size="sm" class="w-[110px] shrink-0" />
                  </div>
                </UFormField>
                <UFormField label="Timeout" required>
                  <div class="flex gap-2 items-center w-full">
                    <UInput v-model="resourceForm.timeoutVal" placeholder="5" size="sm" class="flex-1 min-w-0" />
                    <USelect v-model="resourceForm.timeoutUnit" :items="timeoutUnits" size="sm" class="w-[110px] shrink-0" />
                  </div>
                </UFormField>
              </div>
            </div>
          </div>

          <!-- RIGHT COLUMN: YAML Preview (visible on lg, or on mobile when activeView === 'yaml') -->
          <div 
            v-show="!isMobile || activeView === 'yaml'"
            class="bg-gray-50 dark:bg-carbon-900/10 p-4 sm:p-6 flex flex-col justify-between max-h-[70vh]"
          >
            <div class="flex-1 flex flex-col min-h-0">
              <div class="flex items-center justify-between mb-3 shrink-0">
                <div class="flex items-center gap-2">
                  <UIcon name="i-lucide-terminal" class="w-4 h-4 text-yellow-400" />
                  <span class="font-semibold text-sm text-gray-900 dark:text-wire-200">job.yaml Preview</span>
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
                  <span class="text-xs font-semibold text-gray-500 dark:text-wire-200/60 uppercase tracking-wider">Paste your job.yaml content:</span>
                  <textarea
                    v-model="importContent"
                    class="flex-1 p-2.5 font-mono text-xs bg-gray-50 dark:bg-carbon-900 border border-gray-200 dark:border-carbon-800 rounded-md text-gray-900 dark:text-white focus:outline-hidden focus:ring-1 focus:ring-yellow-400/50 resize-none min-h-[250px]"
                    placeholder="name: my-job&#10;image: ubuntu:latest&#10;command: echo hello&#10;..."
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
                <span>Save this file as <code class="bg-gray-100 dark:bg-carbon-800 px-1 py-0.5 rounded text-xs text-yellow-500">job.yaml</code> inside your Git repository, then create a scheduled job pointing to its path.</span>
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
