<script setup lang="ts">
const route = useRoute()

const tabs = [
  { label: 'Stacks', icon: 'i-lucide-container', value: 'stacks', slot: 'stacks' },
  { label: 'Jobs', icon: 'i-lucide-calendar-clock', value: 'jobs', slot: 'jobs' },
]

const activeTab = ref(route.query.tab === 'jobs' ? 'jobs' : 'stacks')

function onTabChange(val: string | number) {
  activeTab.value = String(val)
  const url = new URL(window.location.href)
  if (val === 'jobs') {
    url.searchParams.set('tab', 'jobs')
  } else {
    url.searchParams.delete('tab')
  }
  window.history.replaceState({}, '', url.toString())
}
</script>

<template>
  <UTabs
    :items="tabs"
    :model-value="activeTab"
    @update:model-value="onTabChange"
  >
    <template #stacks>
      <div class="pt-6">
        <StacksPanel />
      </div>
    </template>
    <template #jobs>
      <div class="pt-6">
        <JobsPanel />
      </div>
    </template>
  </UTabs>
</template>
