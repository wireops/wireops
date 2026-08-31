<script setup lang="ts">
import { computed } from 'vue'
import { stackFleetStatus, stackHasRenderOverrides, stackIsSyncing, stackStatusBadge, stackSyncStatus, stackVisibleDeployStatus, stackWorkerName, stackWorkerStatus } from '../utils/stack-status'

const props = defineProps<{
  stack: any
  workersById: Record<string, any>
}>()

const statusBadge = computed(() => stackStatusBadge(props.stack, props.workersById))
const effectiveStatus = computed(() => stackFleetStatus(props.stack, props.workersById))
const isSyncing = computed(() => stackIsSyncing(props.stack))

function relativeTime(dateStr?: string): string {
  if (!dateStr) return ''
  const diff = Date.now() - new Date(dateStr).getTime()
  if (diff < 0) return 'just now'
  if (diff < 60_000) return `${Math.floor(diff / 1000)}s ago`
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`
  return `${Math.floor(diff / 86_400_000)}d ago`
}

const lastSyncedLabel = computed(() => relativeTime(props.stack?.last_synced_at))
</script>

<template>
  <article
    class="relative w-full min-w-0 overflow-hidden rounded-xl border border-gray-300 border-l-4 bg-white p-3 sm:p-4 shadow-sm transition-all hover:-translate-y-0.5 hover:shadow-[0_0_0_1px_rgba(255,198,0,0.28),0_12px_24px_rgba(15,23,42,0.08)] focus-within:-translate-y-0.5 focus-within:shadow-[0_0_0_2px_rgba(255,198,0,0.42),0_12px_24px_rgba(15,23,42,0.1)] dark:border-carbon-700 dark:bg-carbon-800/55 dark:hover:shadow-[0_0_0_1px_rgba(255,198,0,0.24),0_14px_28px_rgba(0,0,0,0.24)] dark:focus-within:shadow-[0_0_0_2px_rgba(255,198,0,0.42),0_14px_28px_rgba(0,0,0,0.24)]"
    :class="statusBadge.borderClass"
  >
    <div class="relative">
      <div class="absolute right-0 top-0 z-10 flex shrink-0 items-center gap-1.5">
        <UIcon
          v-if="isSyncing"
          name="i-lucide-loader-2"
          class="h-3.5 w-3.5 animate-spin text-gray-400 dark:text-wire-200/40"
          title="Sync in progress"
          aria-label="Sync in progress"
        />
        <OverridedBadge v-if="stackHasRenderOverrides(stack)" />
        <BadgeStatus :status="effectiveStatus" />
      </div>

      <NuxtLink
        :to="`/stacks/${stack.id}`"
        class="group block rounded-md focus:outline-none"
        :aria-label="`Open stack ${stack.name}`"
      >
        <div class="mb-2 sm:mb-3 min-w-0 pr-28">
          <div class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
            <UIcon name="i-lucide-layers" class="h-4 w-4 shrink-0 text-gray-400 dark:text-wire-200/40" />
            <h3 class="truncate text-base font-bold tracking-tight text-gray-950 transition-colors group-hover:text-yellow-500 group-focus-visible:text-yellow-500 dark:text-white">
              {{ stack.name }}
            </h3>
            <UBadge v-if="stack.group" :label="stack.group" color="neutral" variant="outline" size="xs" class="shrink-0" />
          </div>
          <div class="mt-1 flex min-w-0 items-center gap-1.5 text-xs text-gray-500 dark:text-wire-200/50">
            <GitProviderBadge :stack="stack" />
            <template v-if="lastSyncedLabel">
              <span class="shrink-0">·</span>
              <span class="inline-flex shrink-0 items-center gap-1">
                <UIcon name="i-lucide-clock" class="h-3 w-3 shrink-0" />
                {{ lastSyncedLabel }}
              </span>
            </template>
          </div>
        </div>
        <div class="space-y-1 sm:space-y-1.5 rounded-lg bg-gray-50/90 px-2.5 py-2 sm:px-3 sm:py-2.5 transition-colors group-hover:bg-yellow-50/80 group-focus-visible:bg-yellow-50/80 dark:bg-transparent dark:group-hover:bg-transparent dark:group-focus-visible:bg-transparent">
          <div class="grid grid-cols-[78px_1fr] items-start gap-2 text-sm">
            <span class="inline-flex items-center gap-1.5 text-gray-500 dark:text-wire-200/45">
              <UIcon name="i-lucide-refresh-cw" class="h-3.5 w-3.5 shrink-0" />
              Sync
            </span>
            <div class="flex min-w-0 items-center gap-2">
              <UIcon
                :name="stackSyncStatus(stack).icon"
                class="h-3.5 w-3.5 shrink-0"
                :class="stackSyncStatus(stack).iconClass"
              />
              <span
                class="truncate font-medium text-gray-900 dark:text-wire-200"
                :title="stack.last_error_category === 'sync' ? stack.last_error_message : undefined"
              >{{ stackSyncStatus(stack).label }}</span>
            </div>
          </div>

          <div class="grid grid-cols-[78px_1fr] items-start gap-2 text-sm">
            <span class="inline-flex items-center gap-1.5 text-gray-500 dark:text-wire-200/45">
              <UIcon name="i-lucide-rocket" class="h-3.5 w-3.5 shrink-0" />
              Deploy
            </span>
            <div class="flex min-w-0 items-center gap-2">
              <UIcon
                :name="stackVisibleDeployStatus(stack, workersById).icon"
                class="h-3.5 w-3.5 shrink-0"
                :class="stackVisibleDeployStatus(stack, workersById).iconClass"
              />
              <span class="truncate font-medium text-gray-900 dark:text-wire-200">{{ stackVisibleDeployStatus(stack, workersById).label }}</span>
            </div>
          </div>

          <div class="grid grid-cols-[78px_1fr] items-start gap-2 text-sm">
            <span class="inline-flex items-center gap-1.5 text-gray-500 dark:text-wire-200/45">
              <UIcon name="i-lucide-server" class="h-3.5 w-3.5 shrink-0" />
              Worker
            </span>
            <div class="flex min-w-0 items-center gap-2">
              <UTooltip :text="stackWorkerName(stack)">
                <UIcon
                  :name="stackWorkerStatus(stack, workersById).icon"
                  class="h-3.5 w-3.5 shrink-0"
                  :class="stackWorkerStatus(stack, workersById).iconClass"
                  :title="stackWorkerName(stack)"
                  :aria-label="`Worker: ${stackWorkerName(stack)}`"
                />
              </UTooltip>
              <span class="truncate font-medium text-gray-900 dark:text-wire-200">{{ stackWorkerStatus(stack, workersById).label }}</span>
              <UTooltip v-if="stack.expand?.worker?.hostname" :text="stackWorkerName(stack)">
                <span
                  class="inline-flex min-w-0 shrink items-center gap-1 overflow-hidden rounded-md border border-gray-300 px-1.5 py-0.5 text-gray-500 dark:border-carbon-700/60 dark:text-wire-200/55"
                  :title="stackWorkerName(stack)"
                >
                  <UIcon name="i-lucide-server" class="h-3 w-3 shrink-0" aria-hidden="true" />
                  <span class="min-w-0 truncate text-[11px] font-medium">{{ stackWorkerName(stack) }}</span>
                </span>
              </UTooltip>
            </div>
          </div>

          <div v-if="stack.containers_list?.length" class="grid grid-cols-[78px_1fr] items-start gap-2 text-sm">
            <span class="inline-flex items-center gap-1.5 text-gray-500 dark:text-wire-200/45">
              <UIcon name="i-lucide-boxes" class="h-3.5 w-3.5 shrink-0" />
              Services
            </span>
            <div class="min-w-0">
              <StackContainersList :containers="stack.containers_list" />
            </div>
          </div>
        </div>
      </NuxtLink>
    </div>
  </article>
</template>
