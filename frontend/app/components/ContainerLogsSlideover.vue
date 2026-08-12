<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { highlightLogLine } from '~/utils/log-highlighter'

const props = defineProps<{
  stackId: string
  containerId: string
  containerName: string
}>()

const open = defineModel<boolean>('open', { default: false })
const { getContainerLogs } = useApi()
const toast = useToast()

const content = ref('')
const error = ref('')
const loading = ref(false)
const query = ref('')
const wordWrap = ref(false)
const tail = ref('200')
const viewport = ref<HTMLDivElement | null>(null)
let requestId = 0

const tailOptions = [
  { label: 'Last 100 lines', value: '100' },
  { label: 'Last 200 lines', value: '200' },
  { label: 'Last 500 lines', value: '500' },
  { label: 'Last 1,000 lines', value: '1000' },
]

interface DisplayLine {
  number: number
  text: string
  segments: ReturnType<typeof highlightLogLine>
}

const rawLines = computed(() => {
  if (!content.value) return []
  const lines = content.value.replace(/\r\n/g, '\n').split('\n')
  if (lines.at(-1) === '') lines.pop()
  return lines
})

const filteredLines = computed<DisplayLine[]>(() => {
  const needle = query.value.trim().toLocaleLowerCase()
  return rawLines.value
    .map((text, index) => ({ number: index + 1, text }))
    .filter(line => !needle || line.text.toLocaleLowerCase().includes(needle))
    .map(line => ({ ...line, segments: highlightLogLine(line.text, query.value) }))
})

const resultLabel = computed(() => {
  if (loading.value) return 'Loading logs…'
  if (query.value.trim()) {
    return `${filteredLines.value.length} of ${rawLines.value.length} lines`
  }
  return `${rawLines.value.length} line${rawLines.value.length === 1 ? '' : 's'}`
})

async function scrollToBottom() {
  await nextTick()
  if (viewport.value) viewport.value.scrollTop = viewport.value.scrollHeight
}

async function loadLogs() {
  if (!open.value || !props.stackId || !props.containerId) return
  const currentRequest = ++requestId
  loading.value = true
  error.value = ''

  try {
    const response = await getContainerLogs(props.stackId, props.containerId, Number(tail.value))
    if (currentRequest !== requestId) return
    content.value = response.logs || ''
    await scrollToBottom()
  } catch (err: any) {
    if (currentRequest !== requestId) return
    content.value = ''
    error.value = err?.message || 'Failed to load logs'
  } finally {
    if (currentRequest === requestId) loading.value = false
  }
}

function downloadLogs() {
  if (!content.value) return
  const safeName = (props.containerName || 'container')
    .replace(/[^a-z0-9._-]+/gi, '-')
    .replace(/^-+|-+$/g, '') || 'container'
  const timestamp = new Date().toISOString().replace(/[:.]/g, '-')
  const blobUrl = URL.createObjectURL(new Blob([content.value], { type: 'text/plain;charset=utf-8' }))
  const link = document.createElement('a')
  link.href = blobUrl
  link.download = `${safeName}-${timestamp}.log`
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(blobUrl)
  toast.add({ title: 'Logs downloaded', color: 'success' })
}

watch([open, () => props.containerId], ([isOpen, containerId]) => {
  if (!isOpen || !containerId) {
    requestId++
    return
  }
  query.value = ''
  loadLogs()
}, { immediate: true })

watch(tail, () => {
  if (open.value) loadLogs()
})

watch(query, async () => {
  await nextTick()
  if (viewport.value) viewport.value.scrollTop = 0
})
</script>

<template>
  <USlideover
    v-model:open="open"
    title="Container Logs"
    class="w-full max-w-full sm:w-[800px] md:w-[1000px]"
  >
    <template #content>
      <div class="flex h-full min-h-0 flex-col gap-3 p-3 pb-[max(0.75rem,env(safe-area-inset-bottom))] sm:gap-4 sm:p-4">
        <header class="flex shrink-0 items-center justify-between gap-3">
          <div class="min-w-0">
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-scroll-text" class="size-5 shrink-0 text-yellow-400" />
              <h3 class="truncate text-base font-semibold sm:text-lg">{{ containerName }}</h3>
            </div>
            <p class="mt-0.5 pl-7 text-xs text-gray-500 dark:text-wire-200/50">{{ resultLabel }}</p>
          </div>
          <CloseButton class="shrink-0" size="sm" @click="open = false" />
        </header>

        <div class="grid shrink-0 grid-cols-[minmax(0,1fr)_auto] gap-2 sm:grid-cols-[minmax(0,1fr)_auto_auto]">
          <AppTextInput
            v-model="query"
            icon="i-lucide-search"
            aria-label="Search container logs"
            placeholder="Search logs…"
          >
            <template v-if="query" #trailing>
              <button
                type="button"
                class="rounded p-1 text-gray-400 hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-carbon-800 dark:hover:text-wire-200"
                aria-label="Clear log search"
                @click="query = ''"
              >
                <UIcon name="i-lucide-x" class="size-3.5" />
              </button>
            </template>
          </AppTextInput>
          <AppSelectInput
            v-model="tail"
            :items="tailOptions"
            :searchable="false"
            content-width
            aria-label="Number of log lines"
            class="w-36 sm:w-40"
          />
          <div class="col-span-2 grid grid-cols-3 gap-1 sm:col-span-1 sm:flex sm:items-center">
            <UTooltip :text="wordWrap ? 'Disable word wrap' : 'Enable word wrap'">
              <UButton
                :icon="wordWrap ? 'i-lucide-wrap-text' : 'i-lucide-align-left'"
                variant="soft"
                color="neutral"
                class="justify-center"
                aria-label="Toggle log word wrap"
                @click="wordWrap = !wordWrap"
              />
            </UTooltip>
            <UTooltip text="Refresh logs">
              <UButton
                icon="i-lucide-refresh-cw"
                variant="soft"
                color="neutral"
                class="justify-center"
                aria-label="Refresh logs"
                :loading="loading"
                @click="loadLogs"
              />
            </UTooltip>
            <UTooltip text="Download logs">
              <UButton
                icon="i-lucide-download"
                variant="soft"
                color="neutral"
                class="justify-center"
                aria-label="Download logs"
                :disabled="loading || !content"
                @click="downloadLogs"
              />
            </UTooltip>
          </div>
        </div>

        <div class="relative min-h-0 flex-1 overflow-hidden rounded-lg border border-white/10 bg-carbon-950 shadow-inner">
          <div v-if="loading && !content" class="flex h-full min-h-48 items-center justify-center gap-2 text-sm text-wire-200/60">
            <UIcon name="i-lucide-loader-2" class="size-4 animate-spin" />
            Loading logs…
          </div>
          <div v-else-if="error" class="flex h-full min-h-48 flex-col items-center justify-center gap-3 p-6 text-center">
            <UIcon name="i-lucide-circle-alert" class="size-6 text-red-400" />
            <p class="max-w-md text-sm text-red-300">{{ error }}</p>
            <UButton label="Try again" icon="i-lucide-refresh-cw" size="sm" color="error" variant="soft" @click="loadLogs" />
          </div>
          <div v-else-if="!rawLines.length" class="flex h-full min-h-48 items-center justify-center text-sm text-wire-200/50">
            This container has no logs yet.
          </div>
          <div v-else-if="!filteredLines.length" class="flex h-full min-h-48 flex-col items-center justify-center gap-2 p-6 text-center text-wire-200/50">
            <UIcon name="i-lucide-search-x" class="size-6" />
            <p class="text-sm">No lines match “{{ query }}”.</p>
          </div>
          <div
            v-else
            ref="viewport"
            class="h-full overflow-auto overscroll-contain py-2 text-[11px] leading-5 sm:text-xs"
            role="log"
            aria-label="Container log output"
          >
            <div
              v-for="line in filteredLines"
              :key="line.number"
              class="group flex min-w-full hover:bg-white/5"
              :class="wordWrap ? 'items-start' : 'w-max'"
            >
              <span class="sticky left-0 w-10 shrink-0 select-none border-r border-white/10 bg-carbon-950/95 pr-2 text-right font-mono text-wire-200/25 sm:w-12">{{ line.number }}</span>
              <code
                class="block px-3 font-mono text-gray-200"
                :class="wordWrap ? 'min-w-0 flex-1 whitespace-pre-wrap break-words' : 'whitespace-pre'"
              ><template v-for="(segment, index) in line.segments" :key="index"><mark
                v-if="segment.match"
                class="rounded-sm bg-yellow-300 px-0 text-carbon-950"
              >{{ segment.text }}</mark><span
                v-else
                :class="{
                  'text-slate-400': segment.kind === 'timestamp',
                  'font-semibold text-red-400': segment.kind === 'level-error',
                  'font-semibold text-amber-300': segment.kind === 'level-warn',
                  'font-semibold text-sky-300': segment.kind === 'level-info',
                  'text-purple-300': segment.kind === 'level-debug',
                  'text-cyan-300': segment.kind === 'json-key',
                  'text-emerald-300': segment.kind === 'string',
                  'text-violet-300': segment.kind === 'number',
                  'text-blue-300 underline decoration-blue-300/40': segment.kind === 'url',
                }"
              >{{ segment.text }}</span></template></code>
            </div>
          </div>
        </div>
      </div>
    </template>
  </USlideover>
</template>
