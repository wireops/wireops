<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { parseAnsiLine } from '~/utils/ansi'

const props = defineProps<{
  lines: string[]
}>()

// Fixed dark terminal surface regardless of app theme — real color/bold
// codes from docker/compose CLI output only read correctly against a
// consistent dark background, and a terminal panel that flips with the
// app's light/dark toggle would need a second ANSI palette to stay legible.
const parsedLines = computed(() => props.lines.map(parseAnsiLine))

const scrollEl = ref<HTMLPreElement | null>(null)

watch(() => props.lines.length, async () => {
  await nextTick()
  if (scrollEl.value) {
    scrollEl.value.scrollTop = scrollEl.value.scrollHeight
  }
})
</script>

<template>
  <pre
    ref="scrollEl"
    class="rounded-md bg-carbon-950 text-wire-200 p-2.5 text-xs leading-relaxed overflow-y-auto max-h-64 whitespace-pre-wrap break-words"
    style="font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, 'Liberation Mono', monospace;"
  ><span
    v-for="(segments, i) in parsedLines"
    :key="i"
  ><span
    v-for="(seg, j) in segments"
    :key="j"
    :style="{ color: seg.color, fontWeight: seg.bold ? 700 : undefined }"
  >{{ seg.text }}</span>{{ i < parsedLines.length - 1 ? '\n' : '' }}</span></pre>
</template>
