<script setup lang="ts">
const { shouldShowTour, startTour } = useOnboardingTour()

onMounted(() => {
  if (!shouldShowTour.value) return

  nextTick(() => startTour())
})
</script>

<template>
  <div />
</template>
