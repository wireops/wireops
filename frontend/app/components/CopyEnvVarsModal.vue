<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'

const props = defineProps<{
  targetId: string
  targetRepository: string
}>()

const emit = defineEmits<{
  copied: []
  cancel: []
}>()

const { $pb } = useNuxtApp()
const { customGet, customPost } = useApi()
const toast = useToast()

const step = ref<1 | 2>(1)
const stacks = ref<any[]>([])
const sourceStackId = ref('')
const overwrite = ref(false)

const sopsChecking = ref(false)
const sourceHasSops = ref(false)

const sourceVars = ref<{ id: string, key: string, secret: boolean, secret_provider: string }[]>([])
const loadingVars = ref(false)
const selectedKeys = ref<Set<string>>(new Set())

const copying = ref(false)
const copyError = ref('')

onMounted(async () => {
  try {
    stacks.value = await $pb.collection('stacks').getFullList({
      filter: `id != "${props.targetId}"`,
      sort: 'name',
      expand: 'repository',
    })
  } catch {
    stacks.value = []
  }
})

const stackOptions = computed(() =>
  stacks.value.map((s: any) => ({
    label: `${s.name} (${s.expand?.repository?.name || 'unknown repo'})`,
    value: s.id,
  }))
)

const sourceStack = computed(() => stacks.value.find(s => s.id === sourceStackId.value))
const isCrossRepository = computed(() =>
  !!sourceStack.value && sourceStack.value.repository !== props.targetRepository
)

watch(sourceStackId, async () => {
  sourceVars.value = []
  selectedKeys.value = new Set()
  sourceHasSops.value = false
  if (!sourceStackId.value) return

  loadingVars.value = true
  try {
    sourceVars.value = await $pb.collection('stack_env_vars').getFullList({
      filter: `stack = "${sourceStackId.value}"`,
      sort: 'key',
      fields: 'id,key,secret,secret_provider',
    })
  } catch {
    sourceVars.value = []
  } finally {
    loadingVars.value = false
  }

  if (isCrossRepository.value) {
    sopsChecking.value = true
    try {
      const res = await customGet<{ available: boolean }>(`/api/custom/stacks/${sourceStackId.value}/sops-env-vars`)
      sourceHasSops.value = res.available
    } catch {
      sourceHasSops.value = false
    } finally {
      sopsChecking.value = false
    }
  }
})

function toggleKey(key: string) {
  const next = new Set(selectedKeys.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  selectedKeys.value = next
}

const canProceedToKeys = computed(() =>
  !!sourceStackId.value && !sopsChecking.value && !(isCrossRepository.value && sourceHasSops.value)
)

function goToStep2() {
  if (!canProceedToKeys.value) return
  copyError.value = ''
  step.value = 2
}

function goToStep1() {
  step.value = 1
}

async function confirmCopy() {
  if (selectedKeys.value.size === 0) return
  copying.value = true
  copyError.value = ''
  try {
    const res = await customPost<{ copied: number, skipped: string[] }>(
      `/api/custom/stacks/${props.targetId}/env-vars/copy-from`,
      { source_stack: sourceStackId.value, keys: Array.from(selectedKeys.value), overwrite: overwrite.value }
    )
    toast.add({ title: `Copied ${res.copied} variable(s)`, color: 'success' })
    emit('copied')
  } catch (error: any) {
    copyError.value = error?.data?.error || error?.message || 'Failed to copy variables'
  } finally {
    copying.value = false
  }
}
</script>

<template>
  <UCard>
    <template #header>
      <div class="flex items-center gap-2">
        <UIcon name="i-lucide-copy" class="w-5 h-5 text-primary-500" />
        <h2 class="font-semibold">Copy Environment Variables</h2>
        <UBadge :label="`Step ${step} of 2`" variant="subtle" class="ml-auto" />
      </div>
    </template>

    <div v-if="step === 1" class="space-y-4">
      <UFormField label="Source stack" required>
        <AppSelectInput v-model="sourceStackId" :items="stackOptions" placeholder="Select a stack" class="w-full" />
      </UFormField>

      <div v-if="sopsChecking" class="text-sm text-gray-500">Checking for SOPS-managed secrets…</div>

      <div v-else-if="isCrossRepository && sourceHasSops" class="flex items-start gap-3 rounded-lg border border-amber-500/30 bg-amber-500/10 px-4 py-3" role="alert">
        <UIcon name="i-lucide-triangle-alert" class="w-5 h-5 text-amber-500 mt-0.5 shrink-0" />
        <p class="text-sm text-amber-600 dark:text-amber-400">
          This stack's SOPS secrets are tied to its repository and can't be copied across repositories.
        </p>
      </div>

      <div v-else-if="isCrossRepository && sourceStackId" class="flex items-start gap-3 rounded-lg border border-default px-4 py-3">
        <UIcon name="i-lucide-info" class="w-5 h-5 text-gray-400 mt-0.5 shrink-0" />
        <p class="text-sm text-gray-500">Source stack is in a different repository.</p>
      </div>
    </div>

    <div v-else class="space-y-4">
      <p class="text-sm text-gray-500">Select the variables to copy.</p>

      <div v-if="loadingVars" class="text-sm text-gray-500">Loading…</div>
      <div v-else-if="sourceVars.length === 0" class="text-sm text-gray-500">No variables defined on the source stack.</div>
      <div v-else class="divide-y divide-gray-200 dark:divide-gray-800 rounded-lg border border-default max-h-64 overflow-auto">
        <label v-for="env in sourceVars" :key="env.id" class="flex items-center gap-2 px-3 py-2 text-sm cursor-pointer">
          <input type="checkbox" :checked="selectedKeys.has(env.key)" @change="toggleKey(env.key)">
          <span class="font-mono">{{ env.key }}</span>
          <UIcon v-if="env.secret" name="i-lucide-lock" class="h-3.5 w-3.5 text-amber-500" />
        </label>
      </div>

      <label class="flex items-center gap-2 text-sm text-gray-500">
        <input v-model="overwrite" type="checkbox">
        Overwrite existing variables with the same key
      </label>

      <div v-if="copyError" class="flex items-start gap-3 rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3" role="alert" aria-live="assertive">
        <UIcon name="i-lucide-circle-x" class="w-5 h-5 text-red-500 mt-0.5 shrink-0" />
        <p class="text-sm text-red-500">{{ copyError }}</p>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-between gap-2">
        <UButton v-if="step === 2" label="Back" icon="i-lucide-arrow-left" variant="outline" :disabled="copying" @click="goToStep1" />
        <span v-else />

        <div class="flex gap-2">
          <CancelButton :disabled="copying" @click="emit('cancel')" />
          <UButton
            v-if="step === 1"
            label="Continue"
            icon="i-lucide-arrow-right"
            :disabled="!canProceedToKeys"
            @click="goToStep2"
          />
          <UButton
            v-else
            label="Copy Variables"
            icon="i-lucide-copy"
            color="primary"
            :loading="copying"
            :disabled="selectedKeys.size === 0"
            @click="confirmCopy"
          />
        </div>
      </div>
    </template>
  </UCard>
</template>
