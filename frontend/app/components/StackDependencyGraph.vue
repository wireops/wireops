<script setup lang="ts">
import { VueFlow, useVueFlow } from '@vue-flow/core'
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'
import '@vue-flow/controls/dist/style.css'
import { layoutDependencyGraph } from '../utils/dependency-graph-layout'
import type { GraphNode } from '../utils/dependency-graph-layout'

const props = defineProps<{
  stackId: string
}>()

const { getDependencyGraph } = useApi()
const { fitView } = useVueFlow()

const { data: graph, pending, error } = useAsyncData(
  () => `stack_dependency_graph_${props.stackId}`,
  () => getDependencyGraph(props.stackId)
)

const flow = computed(() => {
  if (!graph.value) return { nodes: [], edges: [] }
  return layoutDependencyGraph(graph.value)
})

const isEmpty = computed(() => !pending.value && !error.value && flow.value.nodes.length === 0)

watch(flow, async () => {
  if (flow.value.nodes.length === 0) return
  await nextTick()
  fitView({ padding: 0.2 })
})

const kindIcon: Record<GraphNode['kind'], string> = {
  service: 'i-lucide-box',
  network: 'i-lucide-network',
  volume: 'i-lucide-hard-drive',
}

// Hovering a node highlights it plus everything it's directly wired to
// (dependency, network, volume) and dims the rest -- with dense graphs
// (7+ services, shared networks/volumes) tracing one node's connections by
// eye alone gets lost in the crossing lines.
const hoveredNodeId = ref<string | null>(null)

const connectedNodeIds = computed(() => {
  if (!hoveredNodeId.value || !graph.value) return null
  const ids = new Set<string>()
  for (const edge of graph.value.edges) {
    if (edge.source === hoveredNodeId.value) ids.add(edge.target)
    else if (edge.target === hoveredNodeId.value) ids.add(edge.source)
  }
  return ids
})

const displayNodes = computed(() => flow.value.nodes.map(node => ({
  ...node,
  class: hoveredNodeId.value
    ? (node.id === hoveredNodeId.value || connectedNodeIds.value?.has(node.id) ? 'graph-node-active' : 'graph-node-dim')
    : '',
})))

const displayEdges = computed(() => flow.value.edges.map(edge => ({
  ...edge,
  class: hoveredNodeId.value
    ? (edge.source === hoveredNodeId.value || edge.target === hoveredNodeId.value ? `${edge.class} graph-edge-active` : `${edge.class} graph-edge-dim`)
    : edge.class,
})))

function onNodeMouseEnter({ node }: { node: { id: string } }) {
  hoveredNodeId.value = node.id
}

function onNodeMouseLeave() {
  hoveredNodeId.value = null
}

const statusBorderClass: Record<string, string> = {
  running: 'border-success-400 dark:border-success-500',
  active: 'border-success-400 dark:border-success-500',
  error: 'border-error-400 dark:border-error-500',
  exited: 'border-error-400 dark:border-error-500',
  paused: 'border-warning-400 dark:border-warning-500',
  pending: 'border-warning-400 dark:border-warning-500',
}

function nodeBorderClass(status?: string): string {
  return statusBorderClass[status?.toLowerCase() ?? ''] ?? 'border-gray-200 dark:border-carbon-700'
}
</script>

<template>
  <div class="space-y-3">
    <div class="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs text-gray-500 dark:text-wire-200/50">
      <span class="flex items-center gap-1.5">
        <span class="flex items-center justify-center w-4 h-4 rounded border bg-white dark:bg-carbon-950 border-gray-200 dark:border-carbon-700">
          <UIcon name="i-lucide-box" class="w-2.5 h-2.5 text-gray-400" />
        </span>
        Service
      </span>
      <span class="flex items-center gap-1.5">
        <span class="flex items-center justify-center w-4 h-4 rounded border bg-info-50 dark:bg-info-950/20 border-info-200 dark:border-info-800">
          <UIcon name="i-lucide-network" class="w-2.5 h-2.5 text-info-600 dark:text-info-300" />
        </span>
        Network
      </span>
      <span class="flex items-center gap-1.5">
        <span class="flex items-center justify-center w-4 h-4 rounded border bg-violet-50 dark:bg-violet-950/20 border-violet-200 dark:border-violet-800">
          <UIcon name="i-lucide-hard-drive" class="w-2.5 h-2.5 text-violet-600 dark:text-violet-300" />
        </span>
        Volume
      </span>
      <span class="flex items-center gap-1"><span class="inline-block w-4 border-t-2 border-amber-500" /> Depends On</span>
      <span class="flex items-center gap-1"><span class="inline-block w-4 border-t-2 border-dashed border-info-400" /> Network</span>
      <span class="flex items-center gap-1"><span class="inline-block w-4 border-t-2 border-dashed border-violet-400" /> Volume</span>
    </div>

    <div v-if="pending" class="py-16 flex items-center justify-center">
      <UIcon name="i-lucide-loader-2" class="w-6 h-6 animate-spin text-gray-400" />
    </div>

    <UAlert
      v-else-if="error"
      color="error"
      icon="i-lucide-alert-circle"
      title="Failed to load dependency graph"
      :description="(error as any)?.message"
    />

    <div v-else-if="isEmpty" class="text-center py-16 text-sm text-gray-400 dark:text-wire-200/40">
      No services found in the current compose file.
    </div>

    <div v-else class="h-[520px] rounded-lg border border-gray-200 dark:border-carbon-800 bg-gray-50/50 dark:bg-carbon-900/10">
      <VueFlow
        :nodes="displayNodes"
        :edges="displayEdges"
        :min-zoom="0.2"
        :max-zoom="2"
        fit-view-on-init
        @node-mouse-enter="onNodeMouseEnter"
        @node-mouse-leave="onNodeMouseLeave"
      >
        <Background pattern-color="#9898a8" :gap="16" />
        <Controls position="top-right" />

        <template #node-service="{ data }">
          <div class="px-3 py-2 rounded-lg border-2 bg-white dark:bg-carbon-950 shadow-sm w-[220px]" :class="nodeBorderClass(data.status)">
            <!-- Custom thumbnail: big icon on the left, content stacked on the right -->
            <div v-if="data.slug" class="flex items-center gap-2.5">
              <ContainerIcon
                :name="data.id.replace('service:', '')"
                :slug="data.slug"
                wrapper-class="w-10 h-10 flex shrink-0 items-center justify-center rounded-md bg-gray-100 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 overflow-hidden"
                icon-class="w-7 h-7 object-contain"
              />
              <div class="min-w-0 flex-1 space-y-1">
                <span :title="data.image || undefined" class="block truncate text-xs font-semibold text-gray-900 dark:text-wire-200">{{ data.id.replace('service:', '') }}</span>
                <BadgeStatus v-if="data.status" :status="data.status" />
              </div>
            </div>

            <!-- No custom thumbnail: compact stacked layout with the generic fallback icon -->
            <template v-else>
              <div class="flex items-center gap-2 mb-1.5">
                <ContainerIcon
                  :name="data.id.replace('service:', '')"
                  wrapper-class="w-6 h-6 flex shrink-0 items-center justify-center rounded-md bg-gray-100 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 overflow-hidden"
                  icon-class="w-3.5 h-3.5 object-contain"
                />
                <span :title="data.image || undefined" class="truncate text-xs font-semibold text-gray-900 dark:text-wire-200">{{ data.id.replace('service:', '') }}</span>
              </div>
              <BadgeStatus v-if="data.status" :status="data.status" />
            </template>
          </div>
        </template>

        <template #node-network="{ data }">
          <div class="px-3 py-2 rounded-lg border bg-info-50 dark:bg-info-950/20 border-info-200 dark:border-info-800 min-w-[140px]">
            <div class="flex items-center gap-1.5 text-xs font-semibold text-info-700 dark:text-info-300 mb-1">
              <UIcon :name="kindIcon.network" class="w-3.5 h-3.5 shrink-0" />
              <span class="truncate flex-1">{{ data.id.replace('network:', '') }}</span>
              <UTooltip v-if="data.external" text="External — not owned by this stack">
                <UIcon name="i-lucide-external-link" class="w-3.5 h-3.5 shrink-0 text-warning-500" />
              </UTooltip>
            </div>
            <UBadge v-if="data.external" label="external" color="warning" variant="subtle" size="xs" />
          </div>
        </template>

        <template #node-volume="{ data }">
          <div class="px-3 py-2 rounded-lg border bg-violet-50 dark:bg-violet-950/20 border-violet-200 dark:border-violet-800 min-w-[140px]">
            <div class="flex items-center gap-1.5 text-xs font-semibold text-violet-700 dark:text-violet-300 mb-1">
              <UIcon :name="kindIcon.volume" class="w-3.5 h-3.5 shrink-0" />
              <span class="truncate flex-1">{{ data.id.replace('volume:', '') }}</span>
              <UTooltip v-if="data.external" text="External — not owned by this stack">
                <UIcon name="i-lucide-external-link" class="w-3.5 h-3.5 shrink-0 text-warning-500" />
              </UTooltip>
            </div>
            <UBadge v-if="data.external" label="external" color="warning" variant="subtle" size="xs" />
          </div>
        </template>
      </VueFlow>
    </div>
  </div>
</template>

<style>
/* Unscoped: Vue Flow renders nodes/edges outside this component's scoped
   DOM subtree, so scoped selectors here would never match. */
.graph-node-dim {
  opacity: 0.25;
  transition: opacity 0.15s ease;
}
.graph-node-active {
  opacity: 1;
  transition: opacity 0.15s ease;
  z-index: 10;
}
.graph-edge-dim {
  opacity: 0.15;
  transition: opacity 0.15s ease;
}
.graph-edge-active {
  opacity: 1;
  transition: opacity 0.15s ease;
}

/* @vue-flow/controls ships hardcoded light colors -- override so the
   zoom/fit-view/lock panel follows the app's dark mode instead of always
   rendering as a white box. */
.vue-flow__controls {
  border-radius: 0.5rem;
  overflow: hidden;
  box-shadow: 0 0 0 1px rgb(0 0 0 / 0.08);
}
.vue-flow__controls-button {
  background: #fefefe;
  border-bottom: 1px solid #eee;
  fill: #333;
}
.vue-flow__controls-button:hover {
  background: #f4f4f4;
}
.dark .vue-flow__controls {
  box-shadow: 0 0 0 1px rgb(255 255 255 / 0.08);
}
.dark .vue-flow__controls-button {
  background: var(--ui-bg-elevated, #1c1c22);
  border-bottom: 1px solid var(--ui-border, #333);
  fill: #e5e5e5;
}
.dark .vue-flow__controls-button:hover {
  background: var(--ui-bg-accented, #2a2a33);
}
.dark .vue-flow__controls-button:disabled svg {
  fill-opacity: 0.3;
}
</style>
