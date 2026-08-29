<script setup lang="ts">
// Unlike GithubIcon.vue's single currentColor path (GitHub's mark is
// naturally monochrome, so it can be recolored to match light/dark text),
// GitLab's tanuki mark carries brand-specific two-tone orange shading that a
// hand-traced currentColor path would flatten and misrepresent. This reuses
// the same selfhst/icons asset useRepositoryPlatform's platformIconUrl()
// already serves for every non-GitHub provider, so the logo GitLab actually
// ships is what renders here.
// alt defaults to '' (decorative) since every call site renders this next
// to the "GitLab" name already — only pass a real alt where the icon
// appears with no adjacent text.
const props = withDefaults(defineProps<{
  iconClass?: string
  alt?: string
}>(), {
  iconClass: 'w-5 h-5',
  alt: '',
})

const { platformIconUrl } = useRepositoryPlatform()
</script>

<template>
  <img
    :src="platformIconUrl('gitlab')!"
    :class="props.iconClass"
    :alt="props.alt"
  >
</template>
