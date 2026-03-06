<script setup lang="ts">
import { parseError, getErrorIcon, getErrorColor } from '~/utils/error-parser'

const props = defineProps<{
  error: string
  showRetry?: boolean
  class?: string
}>()

const emit = defineEmits<{
  retry: []
}>()

const parsed = computed(() => parseError(props.error))
const showDetails = ref(false)
</script>

<template>
  <div
:class="['border rounded-lg p-3 space-y-2', props.class, {
    'border-red-200 bg-red-50 dark:border-red-900 dark:bg-red-950': parsed.type !== 'network',
    'border-orange-200 bg-orange-50 dark:border-orange-900 dark:bg-orange-950': parsed.type === 'network'
  }]">
    <div class="flex items-start gap-2">
      <UIcon
:name="getErrorIcon(parsed.type)" class="w-5 h-5 shrink-0 mt-0.5" :class="{
        'text-red-600 dark:text-red-400': parsed.type !== 'network',
        'text-orange-600 dark:text-orange-400': parsed.type === 'network'
      }" />
      <div class="flex-1 min-w-0">
        <p
class="font-semibold text-sm" :class="{
          'text-red-900 dark:text-red-100': parsed.type !== 'network',
          'text-orange-900 dark:text-orange-100': parsed.type === 'network'
        }">
          {{ parsed.message }}
        </p>
        <p
v-if="parsed.suggestion" class="text-xs mt-1" :class="{
          'text-red-700 dark:text-red-300': parsed.type !== 'network',
          'text-orange-700 dark:text-orange-300': parsed.type === 'network'
        }">
          💡 {{ parsed.suggestion }}
        </p>
        <a
v-if="parsed.docLink" :href="parsed.docLink" target="_blank" rel="noopener noreferrer" class="text-xs underline mt-1 inline-block" :class="{
          'text-red-600 dark:text-red-400 hover:text-red-800': parsed.type !== 'network',
          'text-orange-600 dark:text-orange-400 hover:text-orange-800': parsed.type === 'network'
        }">
          📖 View documentation
        </a>
      </div>
      <div class="flex items-center gap-1 shrink-0">
        <UButton 
          v-if="showRetry"
          icon="i-lucide-rotate-cw" 
          size="xs" 
          variant="soft"
          :color="getErrorColor(parsed.type)"
          title="Retry"
          @click="emit('retry')"
        />
        <UButton 
          icon="i-lucide-chevron-down" 
          size="xs" 
          variant="ghost"
          :class="{ 'rotate-180': showDetails }"
          title="Toggle details"
          @click="showDetails = !showDetails"
        />
      </div>
    </div>
    <details v-if="parsed.original" :open="showDetails" class="text-xs">
      <summary
class="cursor-pointer font-medium" :class="{
        'text-red-800 dark:text-red-200': parsed.type !== 'network',
        'text-orange-800 dark:text-orange-200': parsed.type === 'network'
      }">
        Technical details
      </summary>
      <pre
class="mt-2 p-2 rounded text-xs overflow-x-auto whitespace-pre-wrap" :class="{
        'bg-red-100 dark:bg-red-900 text-red-900 dark:text-red-100': parsed.type !== 'network',
        'bg-orange-100 dark:bg-orange-900 text-orange-900 dark:text-orange-100': parsed.type === 'network'
      }">{{ parsed.original }}</pre>
    </details>
  </div>
</template>
