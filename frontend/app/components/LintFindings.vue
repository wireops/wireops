<script setup lang="ts">
import { computed } from 'vue'
import type { LintReport, LintSeverity } from '~/types/lint'

const props = withDefaults(defineProps<{
  report: LintReport | null
  loading?: boolean
  /**
   * Set when `docker compose config` itself failed, so there is no report to
   * show — the file could not be resolved at all.
   */
  configError?: string
  /** Tailwind class for the scrollable findings list's max height. */
  listClass?: string
}>(), {
  loading: false,
  configError: undefined,
  listClass: 'max-h-72',
})

const emit = defineEmits<{ (e: 'select-line', line: number): void }>()

const SEVERITY_META: Record<LintSeverity, {
  color: 'error' | 'warning' | 'info'
  icon: string
  label: string
  iconClass: string
  borderClass: string
  titleBgClass: string
  bodyBgClass: string
}> = {
  error: {
    color: 'error',
    icon: 'i-lucide-octagon-alert',
    label: 'Error',
    iconClass: 'text-red-500',
    borderClass: 'border-red-300 dark:border-red-800/60',
    titleBgClass: 'bg-red-50 dark:bg-red-500/10',
    bodyBgClass: 'bg-red-50/40 dark:bg-red-500/5',
  },
  warning: {
    color: 'warning',
    icon: 'i-lucide-triangle-alert',
    label: 'Warning',
    iconClass: 'text-amber-500',
    borderClass: 'border-amber-300 dark:border-amber-800/60',
    titleBgClass: 'bg-amber-50 dark:bg-amber-500/10',
    bodyBgClass: 'bg-amber-50/40 dark:bg-amber-500/5',
  },
  info: {
    color: 'info',
    icon: 'i-lucide-info',
    label: 'Info',
    iconClass: 'text-blue-500',
    borderClass: 'border-blue-300 dark:border-blue-800/60',
    titleBgClass: 'bg-blue-50 dark:bg-blue-500/10',
    bodyBgClass: 'bg-blue-50/40 dark:bg-blue-500/5',
  },
}

function meta(severity: LintSeverity) {
  return SEVERITY_META[severity] || SEVERITY_META.info
}

const findings = computed(() => props.report?.findings || [])
const hasFindings = computed(() => findings.value.length > 0)

// The server already sorts findings by severity, then rule, then service —
// keep that order rather than re-sorting, so each group's list matches what
// the deploy timeline and the API report.
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

/** One accordion item per severity present, findings kept in report order. */
const groups = computed(() => counts.value.map(([severity, count]) => ({
  severity,
  value: severity,
  label: `${count} ${meta(severity).label.toLowerCase()}${count === 1 ? '' : 's'}`,
  icon: meta(severity).icon,
  findings: findings.value.filter(f => f.severity === severity),
})))

// Errors need immediate attention and start expanded; warnings/infos are
// lower priority and start collapsed to keep the panel scannable.
const openGroups = computed(() => groups.value.filter(g => g.severity === 'error').map(g => g.value))
</script>

<template>
  <div class="space-y-3">
    <div v-if="loading" class="flex items-center gap-2 text-sm text-gray-500 dark:text-wire-400">
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
        <UAccordion
          :items="groups"
          type="multiple"
          :default-value="openGroups"
          class="overflow-y-auto"
          :class="listClass"
        >
          <template #leading="{ item }">
            <UIcon :name="item.icon" :class="['w-4 h-4 shrink-0', meta(item.severity).iconClass]" />
          </template>

          <template #body="{ item }">
            <ul class="space-y-3">
              <li
                v-for="(finding, i) in item.findings"
                :key="`${finding.rule}-${finding.service || ''}-${i}`"
                class="rounded-md border overflow-hidden"
                :class="[
                  meta(finding.severity).borderClass,
                  finding.line ? 'cursor-pointer' : '',
                ]"
                :role="finding.line ? 'button' : undefined"
                :tabindex="finding.line ? 0 : undefined"
                @click="finding.line && emit('select-line', finding.line)"
                @keydown.enter="finding.line && emit('select-line', finding.line)"
                @keydown.space.prevent="finding.line && emit('select-line', finding.line)"
              >
                <div
                  class="flex items-start justify-between gap-2 px-2 py-1"
                  :class="meta(finding.severity).titleBgClass"
                >
                  <p class="text-xs font-medium text-gray-900 dark:text-wire-200">
                    <span class="opacity-50">#{{ i + 1 }}</span> {{ finding.title }}
                  </p>
                  <UBadge
                    v-if="finding.line"
                    :label="`line ${finding.line}`"
                    variant="subtle"
                    color="neutral"
                    size="xs"
                    class="shrink-0"
                  />
                </div>
                <div
                  class="px-2 py-1 space-y-0.5"
                  :class="meta(finding.severity).bodyBgClass"
                >
                  <p class="text-xs text-gray-500 dark:text-gray-400">{{ finding.message }}</p>
                  <p v-if="finding.hint" class="text-xs text-gray-500 dark:text-gray-400">{{ finding.hint }}</p>
                </div>
              </li>
            </ul>
          </template>
        </UAccordion>
      </template>
    </template>
  </div>
</template>
