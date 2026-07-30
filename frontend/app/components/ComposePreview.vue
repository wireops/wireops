<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'

type LintSeverity = 'error' | 'warning' | 'info'

type LintFinding = {
  rule: string
  severity: LintSeverity
  service?: string
  path?: string
  line?: number
  message: string
  hint?: string
}

const props = defineProps<{
  content: string
  filename?: string
  findings: LintFinding[]
}>()

const emit = defineEmits<{ (e: 'select-line', line: number): void }>()

// Rank is severity precedence: a line carrying both an error and a warning is
// marked as an error.
const SEVERITY_RANK: Record<LintSeverity, number> = { error: 0, warning: 1, info: 2 }

const SEVERITY_META: Record<LintSeverity, { icon: string; iconClass: string; rowClass: string }> = {
  error: {
    icon: 'i-lucide-triangle-alert',
    iconClass: 'text-red-500',
    rowClass: 'bg-red-500/10 border-l-2 border-red-500',
  },
  warning: {
    icon: 'i-lucide-triangle-alert',
    iconClass: 'text-amber-500',
    rowClass: 'bg-amber-500/10 border-l-2 border-amber-500',
  },
  info: {
    icon: 'i-lucide-info',
    iconClass: 'text-blue-500',
    rowClass: 'bg-blue-500/10 border-l-2 border-blue-500',
  },
}

// Split on \n after normalising \r\n so a CRLF file does not render a stray
// carriage return at the end of every line.
const lines = computed(() => props.content.replace(/\r\n/g, '\n').split('\n'))

/** line number -> the findings anchored to it, most severe first. */
const findingsByLine = computed(() => {
  const map = new Map<number, LintFinding[]>()
  for (const finding of props.findings) {
    if (!finding.line || finding.line < 1) continue
    const existing = map.get(finding.line)
    if (existing) existing.push(finding)
    else map.set(finding.line, [finding])
  }
  for (const list of map.values()) {
    list.sort((a, b) => SEVERITY_RANK[a.severity] - SEVERITY_RANK[b.severity])
  }
  return map
})

/** Findings the linter could not place on a line, surfaced separately. */
const unplacedCount = computed(() => props.findings.filter(f => !f.line || f.line < 1).length)

function severityFor(lineNumber: number): LintSeverity | null {
  return findingsByLine.value.get(lineNumber)?.[0]?.severity ?? null
}

function meta(severity: LintSeverity) {
  return SEVERITY_META[severity] || SEVERITY_META.info
}

/** Tooltip text for a marked line: every message on it, one per line. */
function tooltipFor(lineNumber: number): string {
  return (findingsByLine.value.get(lineNumber) || []).map(f => f.message).join('\n')
}

const lineRefs = ref<Record<number, HTMLElement | null>>({})
const flashedLine = ref<number | null>(null)

function setLineRef(lineNumber: number, el: unknown) {
  lineRefs.value[lineNumber] = (el as HTMLElement | null) ?? null
}

let flashTimer: ReturnType<typeof setTimeout> | undefined

/**
 * Scrolls the given line into view and flashes it. Exposed rather than driven
 * by a prop so that selecting the same line twice in a row works — a prop
 * would not change value the second time.
 */
function focusOn(line: number) {
  const el = lineRefs.value[line]
  // block: 'center' keeps the line away from the container edges so the
  // surrounding context stays visible.
  el?.scrollIntoView({ block: 'center', behavior: 'smooth' })
  flashedLine.value = line
  clearTimeout(flashTimer)
  flashTimer = setTimeout(() => { flashedLine.value = null }, 1200)
}

// Stale refs would otherwise pin removed rows when a re-lint replaces the file.
watch(() => props.content, () => {
  lineRefs.value = {}
  flashedLine.value = null
  clearTimeout(flashTimer)
})

onBeforeUnmount(() => clearTimeout(flashTimer))

defineExpose({ focusOn })
</script>

<template>
  <div class="rounded-lg border border-gray-200 dark:border-wire-700 overflow-hidden">
    <div
      v-if="filename"
      class="flex items-center gap-2 px-3 py-2 border-b border-gray-200 dark:border-wire-700 bg-gray-50 dark:bg-wire-800/50"
    >
      <UIcon name="i-lucide-file-code" class="w-4 h-4 text-gray-500 shrink-0" />
      <span class="text-xs font-mono text-gray-700 dark:text-wire-200 truncate">{{ filename }}</span>
    </div>

    <div class="max-h-80 overflow-auto">
      <table class="w-full border-collapse font-mono text-xs">
        <tbody>
          <tr
            v-for="(line, i) in lines"
            :key="i"
            :ref="el => setLineRef(i + 1, el)"
            :class="[
              severityFor(i + 1) ? meta(severityFor(i + 1)!).rowClass : '',
              flashedLine === i + 1 ? 'ring-1 ring-inset ring-primary-500' : '',
            ]"
          >
            <td
              class="w-10 select-none text-right pr-2 align-top text-gray-400 dark:text-wire-500 tabular-nums"
              :class="severityFor(i + 1) ? 'cursor-pointer' : ''"
              @click="severityFor(i + 1) && emit('select-line', i + 1)"
            >{{ i + 1 }}</td>

            <td class="w-5 align-top">
              <UTooltip v-if="severityFor(i + 1)" :text="tooltipFor(i + 1)">
                <UIcon
                  :name="meta(severityFor(i + 1)!).icon"
                  :class="['w-3.5 h-3.5 shrink-0 cursor-pointer', meta(severityFor(i + 1)!).iconClass]"
                  @click="emit('select-line', i + 1)"
                />
              </UTooltip>
            </td>

            <!-- Text interpolation only: the file is untrusted repository
                 content and must never be rendered as markup. -->
            <td class="pl-1 pr-3 align-top whitespace-pre text-gray-800 dark:text-wire-100">{{ line }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <p
      v-if="unplacedCount"
      class="px-3 py-2 border-t border-gray-200 dark:border-wire-700 text-xs text-gray-500"
    >
      {{ unplacedCount }} finding{{ unplacedCount === 1 ? '' : 's' }} could not be tied to a specific line.
    </p>
  </div>
</template>
