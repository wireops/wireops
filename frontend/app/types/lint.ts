/**
 * Shape of the compose lint report returned by POST /api/custom/lint/compose.
 *
 * Declared once here because useApi, ComposePreview and LintFindings all speak
 * this contract — three copies would drift silently the first time a field
 * changes server-side.
 */

export type LintSeverity = 'error' | 'warning' | 'info'

export type LintFinding = {
  rule: string
  severity: LintSeverity
  service?: string
  path?: string
  /** 1-based line in the source compose file, or absent when unlocatable. */
  line?: number
  message: string
  hint?: string
}

export type LintReport = {
  findings: LintFinding[]
  errors: number
  warnings: number
  infos: number
}

export type LintComposeBody = {
  repository: string
  compose_path: string
  compose_file: string
  worker?: string
  stack?: string
}

export type LintComposeResponse = {
  report: LintReport
  content?: string
  filename?: string
  config_error?: string
}
