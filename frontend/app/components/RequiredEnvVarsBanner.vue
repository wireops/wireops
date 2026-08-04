<script setup lang="ts">
import { ref, watch, onMounted } from 'vue'

const props = defineProps<{
  stackId: string
  repository: string
  composePath: string
  composeFile: string
  // Passed in from EnvironmentVariablesCard's keysChanged emit — recheck
  // whenever the stack's configured env vars change, since that's exactly
  // what could resolve (or newly break) an unresolved-variable finding.
  envKeys: string[]
}>()

const { lintCompose } = useApi()
const { copy } = useCopy()

const missing = ref<string[]>([])
const checking = ref(false)

let debounceTimer: ReturnType<typeof setTimeout> | undefined

async function check() {
  if (!props.repository) {
    missing.value = []
    return
  }
  checking.value = true
  try {
    const res = await lintCompose({
      repository: props.repository,
      compose_path: props.composePath,
      compose_file: props.composeFile,
      stack: props.stackId,
    })
    const keys = new Set<string>()
    for (const finding of res.report?.findings || []) {
      if (finding.rule !== 'compose/unresolved-variable') continue
      for (const key of finding.vars || []) keys.add(key)
    }
    missing.value = Array.from(keys).sort()
  } catch {
    // Best-effort — a lint failure here shouldn't block the env vars page,
    // it just means the banner stays silent until the next successful check.
    missing.value = []
  } finally {
    checking.value = false
  }
}

function scheduleCheck() {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(check, 500)
}

onMounted(check)
watch(() => props.envKeys, scheduleCheck)
watch(() => [props.repository, props.composePath, props.composeFile], scheduleCheck)

function copyAsEnvLines() {
  copy(missing.value.map(key => `${key}=`).join('\n'), 'Missing variables')
}
</script>

<template>
  <div
    v-if="missing.length"
    class="flex items-start gap-3 rounded-lg border border-amber-500/30 bg-amber-500/10 px-4 py-3"
    role="alert"
  >
    <UIcon name="i-lucide-triangle-alert" class="h-5 w-5 text-amber-500 mt-0.5 shrink-0" />
    <div class="flex-1 min-w-0">
      <p class="text-sm text-amber-700 dark:text-amber-400">
        {{ missing.length }} variable{{ missing.length !== 1 ? 's' : '' }} referenced in the compose file
        {{ missing.length !== 1 ? 'are' : 'is' }} not defined:
        <span class="font-mono">{{ missing.join(', ') }}</span>
      </p>
    </div>
    <UButton label="Copy as KEY=" icon="i-lucide-copy" size="xs" variant="outline" color="warning" @click="copyAsEnvLines" />
  </div>
</template>
