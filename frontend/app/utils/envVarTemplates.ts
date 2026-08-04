export interface EnvVarTemplateVar {
  key: string
  value?: string
  secret?: boolean
}

export interface EnvVarTemplate {
  label: string
  vars: EnvVarTemplateVar[]
}

// Purely client-side, no backend involvement — selecting one pre-fills the
// create form (single var) or the bulk editor (multiple vars) so the user
// still reviews/edits values (e.g. a real password) before saving; nothing
// here is ever written directly to a stack.
export const ENV_VAR_TEMPLATES: EnvVarTemplate[] = [
  {
    label: 'PostgreSQL',
    vars: [
      { key: 'POSTGRES_USER', value: 'postgres' },
      { key: 'POSTGRES_PASSWORD', secret: true },
      { key: 'POSTGRES_DB', value: 'app' },
    ],
  },
  {
    label: 'MySQL',
    vars: [
      { key: 'MYSQL_ROOT_PASSWORD', secret: true },
      { key: 'MYSQL_DATABASE', value: 'app' },
      { key: 'MYSQL_USER', value: 'app' },
      { key: 'MYSQL_PASSWORD', secret: true },
    ],
  },
  {
    label: 'Redis',
    vars: [
      { key: 'REDIS_PASSWORD', secret: true },
    ],
  },
  {
    label: 'Traefik',
    vars: [
      { key: 'TRAEFIK_ACME_EMAIL' },
      { key: 'TRAEFIK_DASHBOARD_AUTH', secret: true },
    ],
  },
  {
    label: 'S3-compatible object storage',
    vars: [
      { key: 'S3_ENDPOINT' },
      { key: 'S3_BUCKET' },
      { key: 'S3_ACCESS_KEY_ID', secret: true },
      { key: 'S3_SECRET_ACCESS_KEY', secret: true },
    ],
  },
  {
    label: 'SMTP',
    vars: [
      { key: 'SMTP_HOST' },
      { key: 'SMTP_PORT', value: '587' },
      { key: 'SMTP_USER' },
      { key: 'SMTP_PASSWORD', secret: true },
    ],
  },
]
