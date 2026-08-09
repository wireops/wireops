<script setup lang="ts">
// UCard variant for containers that hold their own child cards/rows (stack tiles,
// variable rows, worker rows, ...). Sits a shade darker than a plain UCard so the
// nested children read as raised above it instead of blending into a flat white.
</script>

<template>
  <UCard class="bg-gray-50 dark:bg-(--ui-bg)">
    <template v-if="$slots.header" #header>
      <slot name="header" />
    </template>
    <slot />
    <template v-if="$slots.footer" #footer>
      <slot name="footer" />
    </template>
  </UCard>
</template>
