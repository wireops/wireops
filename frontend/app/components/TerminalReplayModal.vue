<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'

const props = defineProps<{
  sessionId: string
  label: string
  exitCode: number
}>()

const open = defineModel<boolean>('open', { default: false })

const history = useTerminalHistory()
const toast = useToast()

const isReplaying = ref(false)
const containerEl = ref<HTMLElement | null>(null)
let term: Terminal | null = null
let fitAddon: FitAddon | null = null

// Bumped whenever the modal closes mid-playback, so already-scheduled
// setTimeout callbacks from a stale replay know to no-op instead of writing
// into a disposed terminal instance.
let replayGeneration = 0

function teardownTerm() {
  term?.dispose()
  term = null
  fitAddon = null
}

// Transcript lines are asciicast-v2 style: first line is a header object,
// each following line is [elapsedSeconds, "o", text]. Gaps are capped so an
// idle terminal in the original session doesn't stall the replay for real.
const MAX_REPLAY_GAP_MS = 1500

// Event text is base64 of the raw pty bytes (see internal/termsession's
// AppendTranscript doc comment) so binary/non-UTF-8 output round-trips
// exactly instead of being mangled by JSON string encoding.
function base64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64)
  const bytes = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i)
  return bytes
}

async function startReplay() {
  replayGeneration++
  const myGeneration = replayGeneration
  isReplaying.value = true
  await nextTick()
  if (!containerEl.value) return

  teardownTerm()
  term = new Terminal({ cursorBlink: false, convertEol: true, disableStdin: true })
  fitAddon = new FitAddon()
  term.loadAddon(fitAddon)
  term.open(containerEl.value)
  fitAddon.fit()
  term.writeln(`[replaying session ${props.sessionId}, exit code ${props.exitCode}]\r\n`)

  try {
    const text = await history.transcript(props.sessionId)
    const lines = text.split('\n').filter(Boolean)
    // First line is the asciicast-v2 header — skip it, everything after is an event.
    let lastT = 0
    let delay = 0
    for (let i = 1; i < lines.length; i++) {
      // One corrupted line (e.g. a transcript file truncated mid-write by a
      // server restart) shouldn't abort the whole replay — skip it and keep
      // scheduling the rest.
      let event: [number, string, string]
      try {
        event = JSON.parse(lines[i]!) as [number, string, string]
      } catch {
        continue
      }
      const gap = Math.min(Math.max(event[0] - lastT, 0) * 1000, MAX_REPLAY_GAP_MS)
      lastT = event[0]
      delay += gap
      const chunk = base64ToBytes(event[2])
      setTimeout(() => {
        if (myGeneration !== replayGeneration) return
        term?.write(chunk)
      }, delay)
    }
    setTimeout(() => {
      if (myGeneration !== replayGeneration) return
      term?.writeln('\r\n[replay finished]')
      isReplaying.value = false
    }, delay)
  } catch (e: any) {
    toast.add({ title: 'Failed to load transcript', description: e?.message, color: 'error' })
    open.value = false
  }
}

function closeModal() {
  replayGeneration++
  teardownTerm()
  open.value = false
}

watch(open, (isOpen) => {
  if (isOpen) {
    startReplay()
  } else {
    replayGeneration++
    isReplaying.value = false
    teardownTerm()
  }
}, { immediate: true })

onBeforeUnmount(() => {
  replayGeneration++
  teardownTerm()
})
</script>

<template>
  <UModal v-model:open="open" :ui="{ content: 'sm:max-w-3xl' }">
    <template #content>
      <div class="p-4 space-y-3">
        <div class="flex items-center justify-between">
          <h3 class="font-semibold text-gray-900 dark:text-wire-200 text-sm">
            Replay — {{ label }}
          </h3>
          <UButton icon="i-lucide-x" variant="ghost" color="neutral" size="xs" aria-label="Close replay" @click="closeModal" />
        </div>
        <div ref="containerEl" class="h-96 bg-black rounded-md overflow-hidden p-1" />
      </div>
    </template>
  </UModal>
</template>
