<script setup lang="ts">
const { $pb } = useNuxtApp()
const toast = useToast()

const props = defineProps<{
  job: any
}>()

const emit = defineEmits<{
  deleted: []
  cancel: []
}>()

const deleting = ref(false)
const errorMsg = ref('')

async function confirmDelete() {
  deleting.value = true
  errorMsg.value = ''
  try {
    await $pb.collection('scheduled_jobs').delete(props.job.id)
    toast.add({ title: 'Job deleted', color: 'success' })
    emit('deleted')
  } catch (e: any) {
    errorMsg.value = e?.message || 'Unexpected error'
  } finally {
    deleting.value = false
  }
}
</script>

<template>
  <UCard>
    <template #header>
      <div class="flex items-center gap-2">
        <UIcon name="i-lucide-trash-2" class="w-5 h-5 text-red-500" />
        <h2 class="font-semibold text-lg text-red-500">Delete Job</h2>
      </div>
    </template>

    <div class="space-y-4">
      <div class="text-sm text-gray-500 space-y-1">
        <p>
          Are you sure you want to delete
          <span class="font-semibold text-gray-800 dark:text-gray-200">{{ job?.job_file }}</span>?
        </p>
        <p class="text-xs">All run history for this job will also be removed.</p>
        <p class="text-xs text-red-500 font-medium">This action cannot be undone.</p>
      </div>

      <div v-if="errorMsg" class="flex items-start gap-3 rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3">
        <UIcon name="i-lucide-circle-x" class="w-5 h-5 text-red-500 mt-0.5 shrink-0" />
        <p class="text-sm text-red-500">{{ errorMsg }}</p>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-2">
        <UButton label="Cancel" variant="outline" @click="emit('cancel')" />
        <UButton
          label="Delete Job"
          color="error"
          icon="i-lucide-trash-2"
          :loading="deleting"
          @click="confirmDelete"
        />
      </div>
    </template>
  </UCard>
</template>
