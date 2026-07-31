<script setup lang="ts">
import { computed } from 'vue'

// Resolves a repository's `platform` field to the right icon component —
// GithubIcon for github, the CDN platform icon (frontend/app/composables/
// useRepositoryPlatform.ts) for any other known platform (gitlab, gitea,
// forgejo, bitbucket, ...), and GenericIcon as the final fallback for an
// unset/unrecognized platform (a plain git-url repository).
const { platformIconUrl } = useRepositoryPlatform()

const props = withDefaults(defineProps<{
  repository?: { platform?: string | null } | null
  iconClass?: string
}>(), {
  repository: null,
  iconClass: undefined,
})

const platform = computed(() => props.repository?.platform || '')
</script>

<template>
  <GithubIcon v-if="platform === 'github'" :icon-class="iconClass" />
  <img
    v-else-if="platformIconUrl(platform)"
    :src="platformIconUrl(platform)!"
    :class="iconClass || 'w-5 h-5 object-contain'"
    alt=""
  >
  <GenericIcon v-else variant="git" :icon-class="iconClass" />
</template>
