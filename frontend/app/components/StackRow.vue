<script setup lang="ts">
import { computed } from 'vue'
import { stackFleetStatus, stackIsSyncing, stackRelativeTime, stackStatusBadge, stackSyncStatus, stackVisibleDeployStatus, stackWorkerName, stackWorkerStatus } from '../utils/stack-status'

const props = defineProps<{
  stack: any
  workersById: Record<string, any>
}>()

const statusBadge = computed(() => stackStatusBadge(props.stack, props.workersById))
const effectiveStatus = computed(() => stackFleetStatus(props.stack, props.workersById))
const isSyncing = computed(() => stackIsSyncing(props.stack))
const lastSyncedLabel = computed(() => stackRelativeTime(props.stack?.last_synced_at))
</script>

<template>
  <NuxtLink
    :to="`/stacks/${stack.id}`"
    :aria-label="`Open stack ${stack.name}`"
    class="group flex w-full min-w-0 items-center gap-3 rounded-lg border border-gray-300 border-l-4 bg-white px-3 py-2 text-sm shadow-sm transition-all hover:shadow-[0_0_0_2px_rgba(255,198,0,0.28),0_4px_12px_rgba(15,23,42,0.06)] focus:outline-none focus-visible:shadow-[0_0_0_2px_rgba(255,198,0,0.42)] dark:border-carbon-700 dark:bg-carbon-800/40"
    :class="statusBadge.borderClass"
  >
    <UIcon
      v-if="isSyncing"
      name="i-lucide-loader-2"
      class="h-3.5 w-3.5 shrink-0 animate-spin text-gray-400 dark:text-wire-200/40"
      aria-hidden="true"
    />
    <UIcon
      v-else
      name="i-lucide-layers"
      class="h-4 w-4 shrink-0 text-gray-400 dark:text-wire-200/40"
      aria-hidden="true"
    />

    <span class="min-w-0 flex-1 truncate font-semibold text-gray-950 group-hover:text-yellow-500 dark:text-white">
      {{ stack.name }}
    </span>

    <UBadge v-if="stack.group" :label="stack.group" color="neutral" variant="outline" size="xs" class="hidden shrink-0 sm:inline-flex" />

    <BadgeStatus :status="effectiveStatus" class="shrink-0" />

    <span class="hidden w-24 shrink-0 items-center gap-1 text-xs text-gray-500 dark:text-wire-200/50 md:inline-flex">
      <UIcon :name="stackSyncStatus(stack).icon" class="h-3.5 w-3.5 shrink-0" :class="stackSyncStatus(stack).iconClass" aria-hidden="true" />
      <span class="truncate">{{ stackSyncStatus(stack).label }}</span>
    </span>

    <span class="hidden w-24 shrink-0 items-center gap-1 text-xs text-gray-500 dark:text-wire-200/50 md:inline-flex">
      <UIcon :name="stackVisibleDeployStatus(stack, workersById).icon" class="h-3.5 w-3.5 shrink-0" :class="stackVisibleDeployStatus(stack, workersById).iconClass" aria-hidden="true" />
      <span class="truncate">{{ stackVisibleDeployStatus(stack, workersById).label }}</span>
    </span>

    <UTooltip :text="stackWorkerName(stack)">
      <span
        class="hidden w-28 shrink-0 items-center gap-1 truncate text-xs text-gray-500 dark:text-wire-200/50 lg:inline-flex"
        :title="stackWorkerName(stack)"
        :aria-label="`Worker: ${stackWorkerName(stack)}`"
      >
        <UIcon :name="stackWorkerStatus(stack, workersById).icon" class="h-3.5 w-3.5 shrink-0" :class="stackWorkerStatus(stack, workersById).iconClass" aria-hidden="true" />
        <span class="truncate">{{ stackWorkerStatus(stack, workersById).label }}</span>
      </span>
    </UTooltip>

    <span v-if="lastSyncedLabel" class="hidden w-16 shrink-0 text-right text-xs text-gray-400 dark:text-wire-200/40 sm:inline">
      {{ lastSyncedLabel }}
    </span>
  </NuxtLink>
</template>
