import { describe, expect, it } from 'vitest'
import { buildStackYaml } from './stack-yaml-generator'

describe('buildStackYaml', () => {
  it('generates the default stack YAML shape', () => {
    expect(buildStackYaml({
      name: 'my-stack',
      timeout: '',
      removeOrphans: true,
      forcePull: false,
      waitRunning: false,
      workerTags: [],
      syncInterval: '',
    })).toBe([
      'version: "wireops.v1"',
      'name: "my-stack"',
      'compose:',
      '  remove_orphans: true',
      '  force_pull: false',
      'jobs:',
      '  wait_running: false',
    ].join('\n'))
  })

  it('includes optional timeout, worker tags and sync interval when set', () => {
    const yaml = buildStackYaml({
      name: 'production-api',
      timeout: '5m',
      removeOrphans: false,
      forcePull: true,
      waitRunning: true,
      workerTags: ['gpu', 'us-east'],
      syncInterval: '30s',
    })

    expect(yaml).toContain('timeout: "5m"')
    expect(yaml).toContain('  remove_orphans: false')
    expect(yaml).toContain('  force_pull: true')
    expect(yaml).toContain('  wait_running: true')
    expect(yaml).toContain('worker:')
    expect(yaml).toContain('  tags:')
    expect(yaml).toContain('    - "gpu"')
    expect(yaml).toContain('    - "us-east"')
    expect(yaml).toContain('sync:')
    expect(yaml).toContain('  interval: "30s"')
  })

  it('serializes untrusted scalar values without manual quote escaping', () => {
    const yaml = buildStackYaml({
      name: 'stack "prod"',
      timeout: '',
      removeOrphans: true,
      forcePull: false,
      waitRunning: false,
      workerTags: ['tag"quoted', 'path\\tag'],
      syncInterval: '',
    })

    expect(yaml).toContain('name: "stack \\"prod\\""')
    expect(yaml).toContain('    - "tag\\"quoted"')
    expect(yaml).toContain('    - "path\\\\tag"')
  })

  it('trims whitespace-only worker tags and sync interval', () => {
    const yaml = buildStackYaml({
      name: 'my-stack',
      timeout: '  ',
      removeOrphans: true,
      forcePull: false,
      waitRunning: false,
      workerTags: ['  ', ' gpu '],
      syncInterval: '   ',
    })

    expect(yaml).not.toContain('timeout:')
    expect(yaml).not.toContain('sync:')
    expect(yaml).toContain('    - "gpu"')
  })
})
