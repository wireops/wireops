<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'

const props = defineProps<{
  stackId: string
  containerId: string
  containerName: string
}>()

const open = defineModel<boolean>('open', { default: false })

const session = useTerminalSession()
const toast = useToast()

// Common shells across base images (Debian/Ubuntu ship bash, Alpine ships
// ash under /bin/sh, some app images add zsh). Custom overrides the preset
// when non-empty, so an unusual image (e.g. a shell-less distroless build,
// or a nonstandard path) is still reachable.
const shellPresets = [
  { label: 'bash', value: '/bin/bash' },
  { label: 'sh', value: '/bin/sh' },
  { label: 'zsh', value: '/bin/zsh' },
  { label: 'ash', value: '/bin/ash' },
]
const selectedShell = ref(shellPresets[0]!.value)
const customShell = ref('')

const phase = ref<'select' | 'terminal'>('select')
const connecting = ref(false)

const containerEl = ref<HTMLElement | null>(null)
let term: Terminal | null = null
let fitAddon: FitAddon | null = null
let resizeObserver: ResizeObserver | null = null

function teardownTerm() {
  resizeObserver?.disconnect()
  resizeObserver = null
  term?.dispose()
  term = null
  fitAddon = null
}

function resetToSelect() {
  session.close()
  teardownTerm()
  phase.value = 'select'
  connecting.value = false
}

async function connect() {
  connecting.value = true
  phase.value = 'terminal'
  await nextTick()
  if (!containerEl.value) {
    connecting.value = false
    return
  }

  term = new Terminal({ cursorBlink: true, convertEol: true })
  fitAddon = new FitAddon()
  term.loadAddon(fitAddon)
  term.open(containerEl.value)
  fitAddon.fit()

  session.onOutput((chunk) => {
    term?.write(chunk)
  })
  session.onClosed((exitCode, reason) => {
    term?.writeln(`\r\n[session closed, exit code ${exitCode}]${reason ? `\r\n${reason}` : ''}`)
  })
  term.onData((data) => {
    session.sendInput(data)
  })

  resizeObserver = new ResizeObserver(() => {
    if (!fitAddon || !term) return
    fitAddon.fit()
    session.resize(term.rows, term.cols)
  })
  resizeObserver.observe(containerEl.value)

  // A custom command may include arguments (e.g. "sh -c 'ls -la'"); split on
  // whitespace so each becomes its own argv element instead of the whole
  // string being passed as a single (nonexistent) executable path. The
  // shell preset is always a single path with no arguments.
  const custom = customShell.value.trim()
  const shell = custom ? custom.split(/\s+/) : [selectedShell.value]

  try {
    await session.open(props.stackId, props.containerId, term.rows, term.cols, shell)
  } catch (e: any) {
    toast.add({ title: 'Failed to open terminal', description: e?.message, color: 'error' })
    resetToSelect()
  } finally {
    connecting.value = false
  }
}

async function closeModal() {
  await session.close()
  teardownTerm()
  open.value = false
}

watch(open, (isOpen) => {
  if (isOpen) {
    phase.value = 'select'
    customShell.value = ''
  } else {
    session.close()
    teardownTerm()
  }
})

onBeforeUnmount(() => {
  session.close()
  teardownTerm()
})
</script>

<template>
  <UModal v-model:open="open" :ui="{ content: 'sm:max-w-3xl' }">
    <template #content>
      <div class="p-4 space-y-3">
        <div class="flex items-center justify-between">
          <h3 class="font-semibold text-gray-900 dark:text-wire-200 text-sm">
            Terminal — {{ containerName }}
          </h3>
          <div class="flex items-center gap-1">
            <UButton
              v-if="phase !== 'select'"
              icon="i-lucide-refresh-cw"
              variant="ghost"
              color="neutral"
              size="xs"
              title="Back to shell selection"
              @click="resetToSelect"
            />
            <UButton icon="i-lucide-x" variant="ghost" color="neutral" size="xs" aria-label="Close terminal" @click="closeModal" />
          </div>
        </div>

        <div v-if="session.error.value" class="text-xs text-red-500">
          {{ session.error.value }}
        </div>

        <div v-if="phase === 'select'" class="space-y-3 py-2">
          <p class="text-xs text-gray-500 dark:text-wire-200/50">
            Choose the shell to run inside {{ containerName }}. If it isn't present in the image, the session will fail to open — pick another one and retry.
          </p>
          <div class="flex items-center gap-2">
            <USelect v-model="selectedShell" :items="shellPresets" :disabled="!!customShell" class="w-32" />
            <span class="text-xs text-gray-400">or</span>
            <UInput v-model="customShell" placeholder="custom command, e.g. /usr/bin/fish" class="flex-1" />
          </div>
          <UButton label="Connect" icon="i-lucide-terminal" :loading="connecting" @click="connect" />
        </div>

        <div v-show="phase === 'terminal'" ref="containerEl" class="h-96 bg-black rounded-md overflow-hidden p-1" />
      </div>
    </template>
  </UModal>
</template>
