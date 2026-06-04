<script setup lang="ts">
import { ref } from 'vue'

const props = defineProps<{
  command: string
}>()

const copied = ref(false)

async function copyToClipboard() {
  try {
    await navigator.clipboard.writeText(props.command)
    copied.value = true
    setTimeout(() => {
      copied.value = false
    }, 2000)
  } catch (e) {
    // best effort
  }
}
</script>

<template>
  <code class="inline-flex items-center gap-1.5 font-mono text-xs px-2 py-1 rounded bg-gray-100 dark:bg-carbon-800 text-gray-900 dark:text-wire-200 border border-gray-200 dark:border-carbon-700/60 break-all">
    <span class="select-all">{{ command }}</span>
    <UTooltip :text="copied ? 'Copied!' : 'Copy command'">
      <button 
        type="button" 
        class="text-gray-400 hover:text-gray-600 dark:hover:text-wire-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:focus-visible:ring-primary-400 rounded transition-colors cursor-pointer"
        @click="copyToClipboard"
      >
        <UIcon :name="copied ? 'i-lucide-check' : 'i-lucide-clipboard'" class="w-3.5 h-3.5" />
      </button>
    </UTooltip>
  </code>
</template>
