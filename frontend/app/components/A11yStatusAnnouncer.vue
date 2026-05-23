<script setup lang="ts">
const { announcements } = useA11yAnnouncer()
</script>

<template>
  <div class="sr-only" aria-live="polite" aria-atomic="true">
    {{ announcements.polite }}
  </div>
  <div class="sr-only" aria-live="assertive" aria-atomic="true">
    {{ announcements.assertive }}
  </div>
</template>
