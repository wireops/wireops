<script setup lang="ts">
import { ref, computed } from 'vue'

const props = withDefaults(defineProps<{
  label: string
  content: string
  buttonLabel?: string
  multiline?: boolean
  downloadFilename?: string
  masked?: boolean
}>(), {
  buttonLabel: 'Copy',
  multiline: false,
  downloadFilename: undefined,
  masked: false,
})

const revealed = ref(false)
const displayContent = computed(() => props.masked && !revealed.value ? '•'.repeat(Math.min(props.content.length, 40)) : props.content)

const toast = useToast()

async function copyToClipboard(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    toast.add({ title: 'Copied!', color: 'success' })
  } catch {
    try {
      const textarea = document.createElement('textarea')
      textarea.value = text
      textarea.style.position = 'fixed'
      document.body.appendChild(textarea)
      textarea.focus()
      textarea.select()
      const successful = document.execCommand('copy')
      document.body.removeChild(textarea)
      if (successful) {
        toast.add({ title: 'Copied!', color: 'success' })
      } else {
        throw new Error()
      }
    } catch {
      toast.add({ title: 'Copy failed', description: 'Please copy the content manually.', color: 'error' })
    }
  }
}

function downloadFile() {
  const blob = new Blob([props.content], { type: 'text/plain' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = props.downloadFilename || 'download.txt'
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}
</script>

<template>
  <UFormField :label="label" class="w-full">
    <div
      v-if="multiline"
      class="relative w-full rounded-lg border border-gray-300 bg-gray-100 p-3 dark:border-carbon-800 dark:bg-carbon-950"
    >
      <div class="absolute right-2 top-2 flex gap-1">
        <AppButtonInput
          v-if="downloadFilename"
          icon="i-lucide-download"
          aria-label="Download"
          size="sm"
          @click="downloadFile"
        />
        <AppButtonInput
          icon="i-lucide-copy"
          aria-label="Copy"
          size="sm"
          @click="copyToClipboard(content)"
        />
      </div>
      <pre class="w-full max-w-full overflow-x-auto whitespace-pre-wrap break-all bg-transparent p-0 pr-16 text-xs font-mono text-gray-800 dark:text-wire-400/80">{{ content }}</pre>
    </div>

    <div v-else class="relative w-full min-w-0 rounded-lg border border-gray-300 bg-gray-100 p-2 dark:border-carbon-700 dark:bg-carbon-800/60">
      <div class="absolute right-2 top-1/2 -translate-y-1/2 flex gap-1">
        <AppButtonInput
          v-if="masked"
          :icon="revealed ? 'i-lucide-eye-off' : 'i-lucide-eye'"
          :aria-label="revealed ? 'Hide' : 'Reveal'"
          size="sm"
          @click="revealed = !revealed"
        />
        <AppButtonInput
          v-if="downloadFilename"
          icon="i-lucide-download"
          aria-label="Download"
          size="sm"
          @click="downloadFile"
        />
        <AppButtonInput
          icon="i-lucide-copy"
          aria-label="Copy"
          size="sm"
          @click="copyToClipboard(content)"
        />
      </div>
      <code class="block min-w-0 overflow-hidden break-all whitespace-pre-wrap pr-24 text-sm font-mono text-wire-400">{{ displayContent }}</code>
    </div>
  </UFormField>
</template>
