<script setup lang="ts">
const props = withDefaults(defineProps<{
  label: string
  content: string
  buttonLabel?: string
  multiline?: boolean
}>(), {
  buttonLabel: 'Copy',
  multiline: false,
})

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
</script>

<template>
  <UFormField :label="label" class="w-full">
    <div
      v-if="multiline"
      class="relative w-full rounded-lg border border-gray-700 bg-gray-900 p-3 dark:border-carbon-800 dark:bg-carbon-950"
    >
      <UButton
        icon="i-lucide-copy"
        variant="outline"
        color="neutral"
        size="sm"
        class="absolute right-2 top-2"
        :ui="{ base: 'shrink-0' }"
        @click="copyToClipboard(content)"
      />
      <pre class="w-full max-w-full overflow-x-auto whitespace-pre-wrap break-all bg-transparent p-0 pr-12 text-xs font-mono text-wire-400/80">{{ content }}</pre>
    </div>

    <div v-else class="relative w-full min-w-0 rounded-lg border border-gray-200 bg-gray-100 p-2 dark:border-carbon-700 dark:bg-carbon-800/60">
      <UButton
        icon="i-lucide-copy"
        variant="ghost"
        color="neutral"
        size="sm"
        class="absolute right-2 top-2"
        :ui="{ base: 'shrink-0' }"
        @click="copyToClipboard(content)"
      />
      <code class="block min-w-0 overflow-hidden break-all whitespace-pre-wrap pr-12 text-sm font-mono text-wire-400">{{ content }}</code>
    </div>
  </UFormField>
</template>
