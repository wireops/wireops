import { describe, it, expect } from 'vitest'
import { parseJobYaml } from './job-yaml-parser'

describe('parseJobYaml', () => {
  it('parses standard flat fields correctly', () => {
    const yaml = `
name: "db-backup"
description: "Backup production database"
cron: "0 0 * * *"
mode: "once"
image: "postgres:15-alpine"
network: "custom-network"
`
    const parsed = parseJobYaml(yaml)
    expect(parsed.name).toBe('db-backup')
    expect(parsed.description).toBe('Backup production database')
    expect(parsed.cron).toBe('0 0 * * *')
    expect(parsed.mode).toBe('once')
    expect(parsed.image).toBe('postgres:15-alpine')
    expect(parsed.network).toBe('custom-network')
  })

  it('parses commands as string or array correctly', () => {
    const yamlStr = 'command: "echo hello"'
    expect(parseJobYaml(yamlStr).command).toBe('echo hello')
    expect(parseJobYaml(yamlStr).commandAsArray).toBe(false)

    const yamlArr = 'command: ["echo", "hello", "world"]'
    expect(parseJobYaml(yamlArr).command).toBe('echo hello world')
    expect(parseJobYaml(yamlArr).commandAsArray).toBe(true)

    // Command containing quoted string (e.g. echo "hello from wireops")
    const yamlQuotedStr = 'command: "echo \\"hello from wireops\\""'
    expect(parseJobYaml(yamlQuotedStr).command).toBe('echo "hello from wireops"')
    expect(parseJobYaml(yamlQuotedStr).commandAsArray).toBe(false)

    const yamlQuotedArr = 'command: ["echo", "hello from wireops"]'
    expect(parseJobYaml(yamlQuotedArr).command).toBe('echo "hello from wireops"')
    expect(parseJobYaml(yamlQuotedArr).commandAsArray).toBe(true)
  })

  it('parses tags and volumes arrays correctly', () => {
    const yaml = `
tags:
  - "production"
  - "database"
volumes:
  - "/var/log:/app/logs"
  - "/data:/app/data"
`
    const parsed = parseJobYaml(yaml)
    expect(parsed.tags).toEqual(['production', 'database'])
    expect(parsed.volumes).toEqual([
      { host: '/var/log', container: '/app/logs' },
      { host: '/data', container: '/app/data' }
    ])
  })

  it('parses nested resources block correctly', () => {
    const yaml = `
resources:
  cpu: "0.5"
  memory: "512m"
  timeout: "10m"
`
    const parsed = parseJobYaml(yaml)
    expect(parsed.cpu).toBe('0.5')
    expect(parsed.memory).toBe('512m')
    expect(parsed.timeout).toBe('10m')
  })
})
