<template>
  <span class="flex items-center gap-3">
    <span class="flex h-10 w-10 shrink-0 items-center justify-center overflow-hidden rounded-2xl">
      <img src="~/assets/img/logo.png" alt="wireops" class="h-7 w-7 object-contain">
    </span>
    <span>
      <span class="block font-black text-base tracking-[0.24em] uppercase text-yellow-400 drop-shadow-[0_0_8px_rgba(255,198,0,0.4)]">
        wireops
      </span>
      <slot />
    </span>
  </span>
</template>
