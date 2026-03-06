<script setup lang="ts">
const { $pb } = useNuxtApp()
const { subscribe } = useRealtime()

const recentLogs = useAsyncData('recent_logs', () =>
  $pb.collection('sync_logs').getList(1, 10, { sort: '-created', expand: 'stack,stack.repository' })
)

onMounted(() => {
  subscribe('sync_logs', () => {
    recentLogs.refresh()
  })
})
</script>

<template>
  <UCard class="h-full w-full">
    <template #header>
      <h2 class="font-semibold">Recent Sync Activity</h2>
    </template>
    <div v-if="recentLogs.data.value?.items?.length" class="divide-y divide-gray-200 dark:divide-gray-800">
      <div
        v-for="log in recentLogs.data.value.items"
        :key="log.id"
        class="flex flex-row items-center justify-between gap-3 py-3"
      >
        <div class="flex items-center gap-3 min-w-0">
          <SyncLogBadge :trigger="log.trigger" :status="log.status" size="sm" class="shrink-0" />
          <div class="min-w-0">
            <p class="text-sm font-medium truncate">
              {{ log.expand?.stack?.name || log.stack }}
            </p>
            <p class="text-xs text-gray-500 font-mono">{{ log.commit_sha?.slice(0, 7) || 'N/A' }}</p>
          </div>
        </div>
        <div class="flex items-center gap-3 text-right text-xs text-gray-500 shrink-0">
          <BadgeStatus :status="log.status" />
          <p>{{ new Date(log.created).toLocaleString() }}</p>
        </div>
      </div>
    </div>
    <p v-else class="text-sm text-gray-500 py-4 text-center">No sync activity yet</p>
  </UCard>
</template>
