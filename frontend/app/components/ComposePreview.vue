<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import type { LintFinding, LintSeverity } from '~/types/lint'

const props = withDefaults(defineProps<{
  content: string
  filename?: string
  findings: LintFinding[]
  /** Tailwind class for the scrollable file area's max height. */
  contentClass?: string
}>(), {
  filename: undefined,
  contentClass: 'max-h-80',
})

const emit = defineEmits<{ (e: 'select-line', line: number): void }>()

const colorMode = useColorMode()

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

function escapeHtml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

// Same rule set as YamlHighlighter, kept in sync so the two previews of a
// compose file always match — this one only adds a leading gutter for
// findings, so it can't reuse that component's <pre> layout directly.
function highlightYamlLine(line: string): string {
  let highlighted = escapeHtml(line)

  if (line.trim().startsWith('#')) {
    return `<span class="yaml-comment">${highlighted}</span>`
  }

  if (/^\s*[\w-]+\s*:/.test(line)) {
    highlighted = highlighted.replace(
      /^(\s*)([\w-]+)(\s*:)/,
      '$1<span class="yaml-key">$2</span>$3',
    )

    const colonIndex = line.indexOf(':')
    const afterColon = colonIndex !== -1 ? line.substring(colonIndex + 1) : undefined
    if (afterColon) {
      const trimmed = afterColon.trim()

      if (/^(['"]).*\1$/.test(trimmed)) {
        highlighted = highlighted.replace(
          /(&quot;|&#39;)(.*?)(&quot;|&#39;)/,
          '<span class="yaml-string">$1$2$3</span>',
        )
      } else if (/^-?\d+(?:\.\d*)?$/.test(trimmed)) {
        highlighted = highlighted.replace(
          /:(\s*)([-0-9.]+)(\s*)$/,
          ':$1<span class="yaml-number">$2</span>$3',
        )
      } else if (/^(true|false|yes|no|on|off)$/i.test(trimmed)) {
        highlighted = highlighted.replace(
          /:(\s*)([a-zA-Z]+)(\s*)$/,
          ':$1<span class="yaml-boolean">$2</span>$3',
        )
      } else if (/^(null|~)$/i.test(trimmed)) {
        highlighted = highlighted.replace(
          /:(\s*)(null|~)(\s*)$/i,
          ':$1<span class="yaml-null">$2</span>$3',
        )
      }
    }
  } else if (/^\s*-\s/.test(line)) {
    highlighted = highlighted.replace(/^(\s*-\s)/, '<span class="yaml-operator">$1</span>')

    if (/(['"]).*\1/.test(line)) {
      highlighted = highlighted.replace(
        /(&quot;|&#39;)(.*?)(&quot;|&#39;)/g,
        '<span class="yaml-string">$1$2$3</span>',
      )
    }
  }

  return highlighted
}

// Split on \n after normalising \r\n so a CRLF file does not render a stray
// carriage return at the end of every line.
const lines = computed(() => props.content.replace(/\r\n/g, '\n').split('\n'))

const highlightedLines = computed(() => lines.value.map(line => highlightYamlLine(line) || '&nbsp;'))

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
  <div
    class="compose-preview rounded-lg border overflow-hidden"
    :class="colorMode.value === 'dark' ? 'dark-mode' : 'light-mode'"
  >
    <div
      v-if="filename"
      class="flex items-center gap-2 px-3 py-2 border-b compose-preview-header"
    >
      <UIcon name="i-lucide-file-code" class="w-4 h-4 shrink-0 compose-preview-filename-icon" />
      <span class="text-xs font-mono truncate compose-preview-filename">{{ filename }}</span>
    </div>

    <div class="overflow-auto" :class="contentClass">
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
              class="w-10 select-none text-right pr-2 align-top tabular-nums compose-preview-linenum"
              :class="severityFor(i + 1) ? 'cursor-pointer' : ''"
              :role="severityFor(i + 1) ? 'button' : undefined"
              :tabindex="severityFor(i + 1) ? 0 : undefined"
              @click="severityFor(i + 1) && emit('select-line', i + 1)"
              @keydown.enter="severityFor(i + 1) && emit('select-line', i + 1)"
              @keydown.space.prevent="severityFor(i + 1) && emit('select-line', i + 1)"
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

            <!-- Highlighted markup is built from an escaped copy of the line
                 (see escapeHtml above) — the untrusted file content itself is
                 never interpreted as markup. -->
            <!-- eslint-disable-next-line vue/no-v-html -->
            <td class="pl-1 pr-3 align-top whitespace-pre compose-preview-line" v-html="highlightedLines[i]" />
          </tr>
        </tbody>
      </table>
    </div>

    <p
      v-if="unplacedCount"
      class="px-3 py-2 border-t text-xs compose-preview-footer"
    >
      {{ unplacedCount }} finding{{ unplacedCount === 1 ? '' : 's' }} could not be tied to a specific line.
    </p>
  </div>
</template>

<style scoped>
.compose-preview {
  border-color: rgb(229 231 235); /* gray-200 */
  background-color: rgb(255 255 255);
  color: rgb(17 24 39); /* gray-900 */
}

.compose-preview-header {
  border-color: rgb(229 231 235); /* gray-200 */
  background-color: rgb(249 250 251); /* gray-50 */
}

.compose-preview-filename {
  color: rgb(55 65 81); /* gray-700 */
}

.compose-preview-filename-icon {
  color: rgb(107 114 128); /* gray-500 */
}

.compose-preview-linenum {
  color: rgb(156 163 175); /* gray-400 */
}

.compose-preview-line {
  color: rgb(31 41 55); /* gray-800 */
}

.compose-preview-footer {
  border-color: rgb(229 231 235); /* gray-200 */
  color: rgb(107 114 128); /* gray-500 */
}

.compose-preview.dark-mode {
  border-color: rgb(31 41 55); /* gray-800 */
  background-color: rgb(3 7 18); /* gray-950 */
  color: rgb(243 244 246); /* gray-100 */
}

.compose-preview.dark-mode .compose-preview-header {
  border-color: rgb(31 41 55); /* gray-800 */
  background-color: rgb(17 24 39 / 0.6); /* gray-900/60 */
}

.compose-preview.dark-mode .compose-preview-filename {
  color: rgb(209 213 219); /* gray-300 */
}

.compose-preview.dark-mode .compose-preview-filename-icon {
  color: rgb(156 163 175); /* gray-400 */
}

.compose-preview.dark-mode .compose-preview-linenum {
  color: rgb(75 85 99); /* gray-600 */
}

.compose-preview.dark-mode .compose-preview-line {
  color: rgb(243 244 246); /* gray-100 */
}

.compose-preview.dark-mode .compose-preview-footer {
  border-color: rgb(31 41 55); /* gray-800 */
  color: rgb(156 163 175); /* gray-400 */
}

/* Keys - yellow to match project primary */
.compose-preview :deep(.yaml-key) {
  color: rgb(202 138 4); /* yellow-600 */
  font-weight: 600;
}

.compose-preview.dark-mode :deep(.yaml-key) {
  color: rgb(250 204 21); /* yellow-400 */
}

/* Strings */
.compose-preview :deep(.yaml-string) {
  color: rgb(22 163 74); /* green-600 */
}

.compose-preview.dark-mode :deep(.yaml-string) {
  color: rgb(74 222 128); /* green-400 */
}

/* Numbers */
.compose-preview :deep(.yaml-number) {
  color: rgb(234 88 12); /* orange-600 */
}

.compose-preview.dark-mode :deep(.yaml-number) {
  color: rgb(251 146 60); /* orange-400 */
}

/* Booleans */
.compose-preview :deep(.yaml-boolean) {
  color: rgb(147 51 234); /* purple-600 */
}

.compose-preview.dark-mode :deep(.yaml-boolean) {
  color: rgb(192 132 252); /* purple-400 */
}

/* Null */
.compose-preview :deep(.yaml-null) {
  color: rgb(107 114 128); /* gray-500 */
  font-style: italic;
}

.compose-preview.dark-mode :deep(.yaml-null) {
  color: rgb(156 163 175); /* gray-400 */
}

/* Comments */
.compose-preview :deep(.yaml-comment) {
  color: rgb(156 163 175); /* gray-400 */
  font-style: italic;
}

.compose-preview.dark-mode :deep(.yaml-comment) {
  color: rgb(107 114 128); /* gray-500 */
}

/* Operators (list dashes) */
.compose-preview :deep(.yaml-operator) {
  color: rgb(37 99 235); /* blue-600 */
  font-weight: bold;
}

.compose-preview.dark-mode :deep(.yaml-operator) {
  color: rgb(96 165 250); /* blue-400 */
}
</style>
