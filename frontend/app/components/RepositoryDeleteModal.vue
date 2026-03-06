<script setup lang="ts">
const props = defineProps<{
  repositoryId: string
  repositoryName: string
}>()

const emit = defineEmits<{
  (e: 'deleted'): void
}>()

const { $pb } = useNuxtApp()
const isOpen = defineModel<boolean>('open', { default: false })
const isDeleting = ref(false)
const errorMessage = ref('')

async function confirmDelete() {
  isDeleting.value = true
  errorMessage.value = ''
  
  try {
    await $pb.collection('repositories').delete(props.repositoryId)
    isOpen.value = false
    emit('deleted')
  } catch (err: any) {
    if (err.data && err.data.message) {
      errorMessage.value = err.data.message
    } else if (err.message) {
      errorMessage.value = err.message
    } else {
      errorMessage.value = 'Failed to delete repository'
    }
  } finally {
    isDeleting.value = false
  }
}

function cancel() {
  isOpen.value = false
}

watch(isOpen, (val) => {
  if (val) {
    errorMessage.value = ''
  }
})
</script>

<template>
  <UModal v-model:open="isOpen">
    <template #content>
      <UCard>
        <template #header>
          <div class="flex items-center gap-2 text-red-600">
            <UIcon name="i-lucide-alert-triangle" class="w-5 h-5" />
            <h3 class="font-semibold text-gray-900 dark:text-white">Delete Repository</h3>
          </div>
        </template>
        
        <div class="space-y-4">
          <p class="text-sm text-gray-500">
            Are you sure you want to delete the repository <span class="font-bold text-gray-900 dark:text-gray-100">{{ repositoryName }}</span>?
          </p>
          <UAlert 
            v-if="errorMessage" 
            color="error" 
            variant="soft" 
            icon="i-lucide-x-circle" 
            :title="errorMessage"
          />
          <p class="text-sm text-gray-400">
            Note: You cannot delete a repository if there are stacks associated with it.
          </p>
        </div>

        <template #footer>
          <div class="flex justify-end gap-2">
            <UButton label="Cancel" variant="outline" @click="cancel" />
            <UButton 
              color="error" 
              label="Delete" 
              :loading="isDeleting" 
              @click="confirmDelete" 
            />
          </div>
        </template>
      </UCard>
    </template>
  </UModal>
</template>
