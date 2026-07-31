<script setup lang="ts">
import { computed } from 'vue'
import { stackRepositorySubtitle, stackSourceStatus } from '../utils/stack-status'

// Compact "where this stack's config comes from" badge — provider icon
// (RepositoryIcon: GithubIcon / CDN platform icon / GenericIcon) plus the
// repository name, or the local-stack hard-drive icon and import path for
// source_type: 'local'. Used on StackCard in place of the plain
// git-branch-icon + text line, so GitHub-backed stacks read as GitHub at a
// glance instead of a generic git dot.
const props = defineProps<{
  stack: any
}>()

const isLocal = computed(() => props.stack?.source_type === 'local')
const repository = computed(() => props.stack?.expand?.repository)
const label = computed(() => stackRepositorySubtitle(props.stack))
const source = computed(() => stackSourceStatus(props.stack))
</script>

<template>
  <span class="inline-flex min-w-0 items-center gap-1.5" :title="source.title">
    <UIcon
      v-if="isLocal"
      :name="source.icon"
      class="h-3.5 w-3.5 shrink-0"
      :class="source.iconClass"
      :aria-label="source.title"
    />
    <RepositoryIcon v-else :repository="repository" icon-class="h-3.5 w-3.5 shrink-0" />
    <span class="truncate">{{ label }}</span>
  </span>
</template>
