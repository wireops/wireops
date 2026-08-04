import { describe, it, expect } from 'vitest'
import { ENV_VAR_TEMPLATES } from './envVarTemplates'

describe('ENV_VAR_TEMPLATES', () => {
  it('has at least one template', () => {
    expect(ENV_VAR_TEMPLATES.length).toBeGreaterThan(0)
  })

  it.each(ENV_VAR_TEMPLATES.map(t => [t.label, t] as const))('%s has at least one var and no duplicate keys', (_label, template) => {
    expect(template.vars.length).toBeGreaterThan(0)
    const keys = template.vars.map(v => v.key)
    expect(new Set(keys).size).toBe(keys.length)
  })
})
