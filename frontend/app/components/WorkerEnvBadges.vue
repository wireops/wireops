<script setup lang="ts">
import { computed } from 'vue'
import { workerHasReportedInfo } from '../utils/worker'
import WorkerOsBadge from './WorkerOsBadge.vue'
import WorkerArchBadge from './WorkerArchBadge.vue'
import WorkerDockerBadge from './WorkerDockerBadge.vue'
import WorkerVersionBadge from './WorkerVersionBadge.vue'
import WorkerComposeBadge from './WorkerComposeBadge.vue'

const props = withDefaults(defineProps<{
  worker: {
    version?: string
    os?: string
    arch?: string
    docker_version?: string
    compose_version?: string
    docker_online?: boolean
  }
  size?: 'xs' | 'sm' | 'md' | 'lg' | 'xl'
  showVersion?: boolean
}>(), {
  size: 'xs',
  showVersion: true,
})

const hasInfo = computed(() => workerHasReportedInfo(props.worker))
</script>

<template>
  <div v-if="hasInfo" class="flex flex-wrap items-center gap-1.5">
    <WorkerOsBadge :os="worker.os" :size="size" />
    <WorkerArchBadge :arch="worker.arch" :size="size" />
    <WorkerDockerBadge :docker-version="worker.docker_version" :docker-online="worker.docker_online" :size="size" />
    <WorkerVersionBadge v-if="showVersion" :version="worker.version" :size="size" />
    <WorkerComposeBadge :compose-version="worker.compose_version" :size="size" />
  </div>
</template>
