<script setup lang="ts">
import { ref, watch, computed } from 'vue'

const { isShowingCommandPalette } = useKeyboard()
const { listJobs, getWorkers } = useApi()
const { $pb } = useNuxtApp()
const router = useRouter()

const loading = ref(false)
const stacks = ref<any[]>([])
const jobs = ref<any[]>([])
const repos = ref<any[]>([])
const workers = ref<any[]>([])

async function loadData() {
  if (stacks.value.length || repos.value.length) return
  loading.value = true
  try {
    const [s, j, r, w] = await Promise.all([
      $pb.collection('stacks').getFullList(),
      listJobs().catch(() => []),
      $pb.collection('repositories').getFullList(),
      getWorkers().catch(() => [])
    ])
    stacks.value = s
    jobs.value = j
    repos.value = r
    workers.value = w
  } catch (err) {
    console.error(err)
  } finally {
    loading.value = false
  }
}

watch(isShowingCommandPalette, (val) => {
  if (val) loadData()
})

const groups = computed(() => {
  return [
    {
      id: 'stacks',
      label: 'Stacks',
      items: stacks.value.map(s => ({
        id: `stack-${s.id}`,
        label: s.name,
        icon: 'i-lucide-layers',
        onSelect: () => {
          isShowingCommandPalette.value = false
          router.push(`/stacks/${s.id}`)
        }
      }))
    },
    {
      id: 'jobs',
      label: 'Jobs',
      items: jobs.value.map(j => ({
        id: `job-${j.id}`,
        label: j.definition?.title || j.id,
        icon: 'i-lucide-calendar-clock',
        onSelect: () => {
          isShowingCommandPalette.value = false
          router.push(`/jobs`)
        }
      }))
    },
    {
      id: 'repos',
      label: 'Repositories',
      items: repos.value.map(r => ({
        id: `repo-${r.id}`,
        label: r.name,
        icon: 'i-lucide-git-branch',
        onSelect: () => {
          isShowingCommandPalette.value = false
          router.push(`/repositories/${r.id}`)
        }
      }))
    },
    {
      id: 'workers',
      label: 'Workers',
      items: workers.value.filter((w: any) => w.status === 'ACTIVE').map((w: any) => ({
        id: `worker-${w.id}`,
        label: w.hostname,
        icon: 'i-lucide-network',
        onSelect: () => {
          isShowingCommandPalette.value = false
          router.push(`/workers`)
        }
      }))
    }
  ].filter(g => g.items.length > 0)
})
</script>

<template>
  <UModal v-model:open="isShowingCommandPalette">
    <template #content>
      <UCommandPalette
        :loading="loading"
        :groups="groups"
        :autoselect="false"
        placeholder="Search stacks, jobs, repos, workers..."
        class="flex-1 h-[400px]"
      />
    </template>
  </UModal>
</template>
