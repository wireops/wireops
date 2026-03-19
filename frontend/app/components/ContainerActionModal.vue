<script setup lang="ts">
const props = defineProps<{
  stackId: string
  containerId: string
  containerName: string
  action: 'stop' | 'restart' | null
}>()

const open = defineModel<boolean>('open', { default: false })
const emit = defineEmits(['done'])

const { stopContainer, restartContainer } = useApi()
const toast = useToast()
const loading = ref(false)

const title = computed(() => {
  if (props.action === 'stop') return 'Stop Container'
  if (props.action === 'restart') return 'Restart Container'
  return ''
})

const description = computed(() => {
  const actionText = props.action === 'stop' ? 'stop' : 'restart'
  return `Are you sure you want to ${actionText} the container ${props.containerName}?`
})

const confirmLabel = computed(() => {
  if (props.action === 'stop') return 'Stop'
  if (props.action === 'restart') return 'Restart'
  return 'Confirm'
})

const confirmColor = computed(() => {
  return props.action === 'stop' ? 'warning' : 'info'
})

async function onConfirm() {
  if (!props.action || !props.containerId) return
  
  loading.value = true
  try {
    if (props.action === 'stop') {
      await stopContainer(props.stackId, props.containerId)
      toast.add({ title: 'Container stopped', color: 'warning' })
    } else {
      await restartContainer(props.stackId, props.containerId)
      toast.add({ title: 'Container restarted', color: 'success' })
    }
    open.value = false
    emit('done')
  } catch (e: any) {
    toast.add({ title: `Failed to ${props.action} container`, description: e?.message, color: 'error' })
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <UModal v-model:open="open">
    <template #content>
      <div class="p-6 space-y-5">
        <div class="flex items-start gap-4">
          <div 
            class="flex items-center justify-center w-10 h-10 rounded-lg shrink-0"
            :class="action === 'stop' ? 'bg-yellow-400/10' : 'bg-blue-400/10'"
          >
            <UIcon 
              :name="action === 'stop' ? 'i-lucide-square' : 'i-lucide-rotate-cw'" 
              class="w-5 h-5"
              :class="action === 'stop' ? 'text-yellow-400' : 'text-blue-400'"
            />
          </div>
          <div>
            <h3 class="font-semibold text-gray-900 dark:text-wire-200 text-base">{{ title }}</h3>
            <p class="text-sm text-gray-500 dark:text-wire-200/50 mt-1">
              {{ description }}
            </p>
          </div>
        </div>
        <div class="flex justify-end gap-2 pt-1">
          <UButton 
            label="Cancel" 
            variant="outline" 
            color="neutral" 
            :disabled="loading" 
            @click="open = false" 
          />
          <UButton 
            :label="confirmLabel" 
            :color="confirmColor" 
            :loading="loading" 
            @click="onConfirm" 
          />
        </div>
      </div>
    </template>
  </UModal>
</template>
