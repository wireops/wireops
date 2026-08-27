<script setup lang="ts">
withDefaults(defineProps<{ collapsed?: boolean }>(), {
  collapsed: false,
})
</script>

<template>
  <span class="flex items-center gap-3">
    <span class="flex h-10 w-10 shrink-0 items-center justify-center overflow-hidden rounded-2xl">
      <img src="~/assets/img/logo.png" alt="wireops" class="h-7 w-7 object-contain">
    </span>
    <span v-show="!collapsed" class="whitespace-nowrap">
      <span class="block">
        <span class="inline-block bg-linear-to-r from-yellow-400 via-[#d5cc83] to-wire-400 bg-clip-text font-black text-lg tracking-[0.06em] text-transparent drop-shadow-[0_0_8px_rgba(255,198,0,0.28)]">
          wireops
        </span>
      </span>
      <slot />
    </span>
  </span>
</template>
