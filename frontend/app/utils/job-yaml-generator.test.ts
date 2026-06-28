import { describe, expect, it } from 'vitest'
import { buildJobYaml } from './job-yaml-generator'

describe('buildJobYaml', () => {
  it('generates the default scheduled job YAML shape', () => {
    expect(buildJobYaml({
      name: 'my-scheduled-job',
      description: 'A brief description of what this job does',
      cron: '*/5 * * * *',
      tags: ['production', 'cleanup'],
      mode: 'once',
      image: 'ubuntu:latest',
      command: 'echo "hello from wireops"',
      commandAsArray: false,
      volumes: [{ host: '/var/log', container: '/app/logs' }],
      network: '',
      cpu: '0.5',
      memory: '512m',
      timeout: '5m',
    })).toBe([
      'name: "my-scheduled-job"',
      'description: "A brief description of what this job does"',
      'cron: "*/5 * * * *"',
      'tags:',
      '  - "production"',
      '  - "cleanup"',
      'mode: "once"',
      'image: "ubuntu:latest"',
      'command: "echo \\"hello from wireops\\""',
      'volumes:',
      '  - "/var/log:/app/logs"',
      'resources:',
      '  cpu: "0.5"',
      '  memory: "512m"',
      '  timeout: "5m"',
    ].join('\n'))
  })

  it('serializes untrusted scalar values without manual quote escaping', () => {
    const yaml = buildJobYaml({
      name: 'backup "primary"',
      description: 'copy C:\\tmp\\jobs\nthen notify',
      cron: '0 0 * * *',
      tags: ['prod"blue', 'path\\tag'],
      mode: 'once_all',
      image: 'alpine:3.20',
      command: 'sh -c "printf \\"done\\""',
      commandAsArray: false,
      volumes: [{ host: '/host "logs"', container: '/app\\logs' }],
      network: 'jobs "net"',
      cpu: '1',
      memory: '1g',
      timeout: '30m',
    })

    expect(yaml).toContain('name: "backup \\"primary\\""')
    expect(yaml).toContain('description: "copy C:\\\\tmp\\\\jobs\\nthen notify"')
    expect(yaml).toContain('  - "prod\\"blue"')
    expect(yaml).toContain('  - "path\\\\tag"')
    expect(yaml).toContain('command: "sh -c \\"printf \\\\\\"done\\\\\\"\\""')
    expect(yaml).toContain('  - "/host \\"logs\\":/app\\\\logs"')
    expect(yaml).toContain('network: "jobs \\"net\\""')
  })

  it('can generate command as an inline YAML array', () => {
    const yaml = buildJobYaml({
      name: 'array-command',
      description: 'Uses argv format',
      cron: '* * * * *',
      tags: [],
      mode: 'once',
      image: 'ubuntu:latest',
      command: 'echo "hello world"',
      commandAsArray: true,
      volumes: [],
      network: '',
      cpu: '100m',
      memory: '128m',
      timeout: '1m',
    })

    expect(yaml).toContain('tags: []')
    expect(yaml).toContain('command: ["echo", "hello world"]')
    expect(yaml).toContain('volumes: []')
  })
})
