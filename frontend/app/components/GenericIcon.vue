<script setup lang="ts">
// Generic/"any git server" marker used next to GithubIcon.vue for the
// "Generic" source card — inlined Lucide glyphs so it follows the same
// component pattern as GithubIcon. `variant="git"` swaps the plain globe
// for a git-branch glyph when the generic context is specifically about
// a raw git URL rather than "any remote server".
withDefaults(defineProps<{
  iconClass?: string
  variant?: 'globe' | 'git'
}>(), {
  iconClass: undefined,
  variant: 'globe',
})
</script>

<template>
  <svg
    v-if="variant === 'git'"
    xmlns="http://www.w3.org/2000/svg"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="2"
    stroke-linecap="round"
    stroke-linejoin="round"
    :class="iconClass || 'w-5 h-5 text-gray-400'"
    aria-hidden="true"
  >
    <line x1="6" x2="6" y1="3" y2="15" />
    <circle cx="18" cy="6" r="3" />
    <circle cx="6" cy="18" r="3" />
    <path d="M18 9a9 9 0 0 1-9 9" />
  </svg>
  <svg
    v-else
    xmlns="http://www.w3.org/2000/svg"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="2"
    stroke-linecap="round"
    stroke-linejoin="round"
    :class="iconClass || 'w-5 h-5 text-gray-400'"
    aria-hidden="true"
  >
    <circle cx="12" cy="12" r="10" />
    <path d="M2 12h20" />
    <path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z" />
  </svg>
</template>
