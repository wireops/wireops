<script setup lang="ts">
// Mirrors ConnectGithubButton.vue: extracted so the OAuth popup flow can be
// triggered both from RepositoryCreateModal.vue's org/repo picker and from
// the GitLab card's dialog on Settings → Integrations, without duplicating
// the click handler.
const props = withDefaults(defineProps<{
  label?: string
}>(), {
  label: 'Connect GitLab',
})

const emit = defineEmits<{
  connected: [{ keyId: string, login: string }]
}>()

const { connect } = useGitProviderOAuth()
const toast = useToast()
const connecting = ref(false)

async function handleClick() {
  connecting.value = true
  try {
    const result = await connect('gitlab')
    if (!result) {
      toast.add({ title: 'Connection cancelled or failed', color: 'warning' })
      return
    }
    emit('connected', result)
  } catch (error: any) {
    toast.add({ title: 'Failed to connect', description: error?.message || 'Unknown error', color: 'error' })
  } finally {
    connecting.value = false
  }
}
</script>

<template>
  <UButton
    :label="props.label"
    icon="i-lucide-gitlab"
    variant="outline"
    color="neutral"
    :loading="connecting"
    @click="handleClick"
  />
</template>
