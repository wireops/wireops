import { describe, it, expect } from 'vitest'
import { parseStackYaml } from './stack-yaml-parser'

describe('parseStackYaml', () => {
  it('parses standard flat fields correctly', () => {
    const yaml = `
version: "wireops.v1"
name: "production-api"
timeout: "5m"
`
    const parsed = parseStackYaml(yaml)
    expect(parsed.name).toBe('production-api')
    expect(parsed.timeout).toBe('5m')
  })

  it('parses compose block booleans correctly', () => {
    const yaml = `
compose:
  remove_orphans: false
  force_pull: true
`
    const parsed = parseStackYaml(yaml)
    expect(parsed.removeOrphans).toBe(false)
    expect(parsed.forcePull).toBe(true)
  })

  it('parses jobs block correctly', () => {
    const yaml = `
jobs:
  wait_running: true
`
    const parsed = parseStackYaml(yaml)
    expect(parsed.waitRunning).toBe(true)
  })

  it('parses worker tags array correctly', () => {
    const yaml = `
worker:
  tags:
    - "gpu"
    - "us-east"
`
    const parsed = parseStackYaml(yaml)
    expect(parsed.workerTags).toEqual(['gpu', 'us-east'])
  })

  it('parses sync interval correctly', () => {
    const yaml = `
sync:
  interval: "30s"
`
    const parsed = parseStackYaml(yaml)
    expect(parsed.syncInterval).toBe('30s')
  })

  it('parses a full wireops.yaml document', () => {
    const yaml = `
version: "wireops.v1"
name: "my-stack"
timeout: "10m"
compose:
  remove_orphans: true
  force_pull: false
jobs:
  wait_running: false
worker:
  tags:
    - "prod"
sync:
  interval: "1m"
`
    const parsed = parseStackYaml(yaml)
    expect(parsed).toEqual({
      name: 'my-stack',
      timeout: '10m',
      removeOrphans: true,
      forcePull: false,
      waitRunning: false,
      workerTags: ['prod'],
      syncInterval: '1m',
    })
  })
})
