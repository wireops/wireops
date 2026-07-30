<script setup lang="ts">
import { computed } from 'vue'

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

type LintReport = {
  findings: LintFinding[]
  errors: number
  warnings: number
  infos: number
}

const props = defineProps<{
  report: LintReport | null
  loading?: boolean
  /**
   * Set when `docker compose config` itself failed, so there is no report to
   * show — the file could not be resolved at all.
   */
  configError?: string
}>()

const emit = defineEmits<{ (e: 'select-line', line: number): void }>()

const SEVERITY_META: Record<LintSeverity, { color: 'error' | 'warning' | 'info'; icon: string; label: string }> = {
  error: { color: 'error', icon: 'i-lucide-octagon-alert', label: 'Error' },
  warning: { color: 'warning', icon: 'i-lucide-triangle-alert', label: 'Warning' },
  info: { color: 'info', icon: 'i-lucide-info', label: 'Info' },
}

function meta(severity: LintSeverity) {
  return SEVERITY_META[severity] || SEVERITY_META.info
}

const findings = computed(() => props.report?.findings || [])
const hasFindings = computed(() => findings.value.length > 0)

// The server already sorts findings by severity, then rule, then service —
// keep that order rather than re-sorting, so the list matches what the deploy
// timeline and the API report.
const counts = computed(() => {
  const r = props.report
  if (!r) return []
  return ([
    ['error', r.errors],
    ['warning', r.warnings],
    ['info', r.infos],
  ] as [LintSeverity, number][])
    .filter(([, n]) => n > 0)
})
</script>

<template>
  <div class="space-y-3">
    <div v-if="loading" class="flex items-center gap-2 text-sm text-gray-500">
      <UIcon name="i-lucide-loader-2" class="w-4 h-4 animate-spin" />
      Checking compose file...
    </div>

    <UAlert
      v-else-if="configError"
      color="error"
      icon="i-lucide-file-x"
      title="Could not read the compose file"
      :description="configError"
    />

    <template v-else-if="report">
      <UAlert
        v-if="!hasFindings"
        color="success"
        icon="i-lucide-check-circle-2"
        title="No issues found"
        description="This compose file passed every check."
      />

      <template v-else>
        <div class="flex flex-wrap items-center gap-1.5">
          <UBadge
            v-for="[severity, count] in counts"
            :key="severity"
            :color="meta(severity).color"
            :label="`${count} ${meta(severity).label.toLowerCase()}${count === 1 ? '' : 's'}`"
            variant="subtle"
            size="xs"
          />
        </div>

        <ul class="space-y-2 max-h-72 overflow-y-auto">
          <li
            v-for="(finding, i) in findings"
            :key="`${finding.rule}-${finding.service || ''}-${i}`"
            class="rounded-lg border border-gray-200 dark:border-wire-700 p-3 space-y-1"
            :class="finding.line ? 'cursor-pointer hover:border-primary-400 dark:hover:border-primary-500' : ''"
            @click="finding.line && emit('select-line', finding.line)"
          >
            <div class="flex items-start gap-2">
              <UIcon
                :name="meta(finding.severity).icon"
                :class="[
                  'w-4 h-4 shrink-0 mt-0.5',
                  finding.severity === 'error' ? 'text-red-500'
                  : finding.severity === 'warning' ? 'text-amber-500'
                    : 'text-blue-500',
                ]"
              />
              <div class="min-w-0 flex-1 space-y-1">
                <p class="text-sm text-gray-900 dark:text-wire-100">{{ finding.message }}</p>
                <p v-if="finding.hint" class="text-xs text-gray-500 dark:text-wire-400">{{ finding.hint }}</p>
                <div class="flex flex-wrap items-center gap-1.5">
                  <UBadge :label="finding.rule" variant="outline" color="neutral" size="xs" />
                  <UBadge
                    v-if="finding.line"
                    :label="`line ${finding.line}`"
                    variant="subtle"
                    color="neutral"
                    size="xs"
                  />
                  <span v-if="finding.path" class="text-xs font-mono text-gray-400 truncate">{{ finding.path }}</span>
                </div>
              </div>
            </div>
          </li>
        </ul>
      </template>
    </template>
  </div>
</template>
