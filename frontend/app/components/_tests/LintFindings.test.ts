import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import LintFindings from '../LintFindings.vue'

// Stubs are built with setup() + h() rather than template HTML strings —
// Codacy flags inline HTML-string stubs as XSS even in test-only code.
const stubs = {
  UAlert: {
    props: ['title', 'description', 'color', 'icon'],
    setup(props: { title?: string; description?: string; color?: string }) {
      return () => h('div', { class: `alert alert-${props.color}` }, [
        h('div', { class: 'alert-title' }, props.title),
        h('div', { class: 'alert-description' }, props.description),
      ])
    },
  },
  UBadge: {
    props: ['label', 'color', 'variant', 'size'],
    setup(props: { label?: string }) {
      return () => h('span', { class: 'badge' }, props.label)
    },
  },
  UIcon: {
    props: ['name'],
    setup(props: { name?: string }) {
      return () => h('span', { class: 'icon', 'data-name': props.name })
    },
  },
  // Renders every item's header + body unconditionally, standing in for
  // reka-ui's real collapsible state so groups are inspectable without
  // simulating expand/collapse.
  UAccordion: {
    props: ['items'],
    setup(props: { items?: { label?: string }[] }, { slots }: { slots: Record<string, (arg: unknown) => unknown> }) {
      return () => h('div', { class: 'accordion' }, (props.items || []).map((item, index) =>
        h('div', { class: 'accordion-item', key: index }, [
          h('div', { class: 'accordion-trigger' }, [
            slots.leading?.({ item }),
            item.label,
          ]),
          h('div', { class: 'accordion-body' }, slots.body?.({ item })),
        ]),
      ))
    },
  },
}

function mountFindings(props: Record<string, unknown>) {
  return mount(LintFindings, { props, global: { stubs } })
}

const emptyReport = { findings: [], errors: 0, warnings: 0, infos: 0 }

describe('LintFindings', () => {
  it('shows a loading state and nothing else while checking', () => {
    const wrapper = mountFindings({ report: null, loading: true })

    expect(wrapper.text()).toContain('Checking compose file')
    expect(wrapper.find('.alert').exists()).toBe(false)
  })

  it('reports a clean file when the report has no findings', () => {
    const wrapper = mountFindings({ report: emptyReport })

    expect(wrapper.find('.alert-title').text()).toBe('No issues found')
    expect(wrapper.findAll('li')).toHaveLength(0)
  })

  it('shows the config error instead of a report when compose could not be read', () => {
    const wrapper = mountFindings({
      report: emptyReport,
      configError: 'failed to resolve compose config: yaml: line 3: bad indent',
    })

    expect(wrapper.find('.alert-title').text()).toBe('Could not read the compose file')
    expect(wrapper.find('.alert-description').text()).toContain('line 3')
    // The empty report must not also render its "no issues" success alert.
    expect(wrapper.text()).not.toContain('No issues found')
  })

  it('renders each finding with its message, hint, rule and path', () => {
    const wrapper = mountFindings({
      report: {
        findings: [
          {
            rule: 'policy/images',
            title: 'Image blocked by policy',
            severity: 'error',
            service: 'web',
            path: 'services.web.image',
            message: 'image policy violation: image "nginx:latest" uses :latest',
            hint: 'blocked by this worker\'s deploy policy',
          },
          {
            rule: 'compose/no-restart-policy',
            title: 'No restart policy',
            severity: 'warning',
            service: 'db',
            path: 'services.db.restart',
            message: 'service "db" has no restart policy',
          },
        ],
        errors: 1,
        warnings: 1,
        infos: 0,
      },
    })

    const items = wrapper.findAll('li')
    expect(items).toHaveLength(2)
    expect(items[0]!.text()).toContain('Image blocked by policy')
    expect(items[0]!.text()).toContain('uses :latest')
    expect(items[0]!.text()).toContain('deploy policy')
    expect(items[0]!.text()).not.toContain('policy/images')
    expect(items[0]!.text()).not.toContain('services.web.image')
    expect(items[1]!.text()).toContain('No restart policy')
  })

  it('groups findings into one accordion section per severity, omitting severities with none', () => {
    const wrapper = mountFindings({
      report: {
        findings: [
          { rule: 'compose/latest-tag', severity: 'warning', message: 'a' },
          { rule: 'compose/latest-tag', severity: 'warning', message: 'b' },
          { rule: 'compose/no-healthcheck', severity: 'info', message: 'c' },
        ],
        errors: 0,
        warnings: 2,
        infos: 1,
      },
    })

    const groups = wrapper.findAll('.accordion-item')
    expect(groups).toHaveLength(2)

    const triggers = wrapper.findAll('.accordion-trigger').map(t => t.text())
    expect(triggers).toContain('2 warnings')
    expect(triggers).toContain('1 info')
    expect(triggers.some(t => t.includes('error'))).toBe(false)

    const warningGroup = groups.find(g => g.text().startsWith('2 warnings'))!
    expect(warningGroup.findAll('li')).toHaveLength(2)
  })

  it('makes findings with a line keyboard-operable, and leaves the rest inert', async () => {
    const wrapper = mountFindings({
      report: {
        findings: [
          { rule: 'policy/images', severity: 'error', line: 7, message: 'has a line' },
          { rule: 'compose/no-services', severity: 'error', message: 'no line' },
        ],
        errors: 2,
        warnings: 0,
        infos: 0,
      },
    })

    const items = wrapper.findAll('li')
    expect(items[0]!.attributes('role')).toBe('button')
    expect(items[0]!.attributes('tabindex')).toBe('0')
    await items[0]!.trigger('keydown.enter')
    await items[0]!.trigger('keydown.space')
    expect(wrapper.emitted('select-line')).toEqual([[7], [7]])

    // A finding with no line has nothing to jump to, so it must not advertise
    // itself as actionable.
    expect(items[1]!.attributes('role')).toBeUndefined()
    expect(items[1]!.attributes('tabindex')).toBeUndefined()
    await items[1]!.trigger('keydown.enter')
    expect(wrapper.emitted('select-line')).toEqual([[7], [7]])
  })

  it('preserves the server ordering rather than re-sorting', () => {
    const wrapper = mountFindings({
      report: {
        findings: [
          { rule: 'policy/images', severity: 'error', message: 'first' },
          { rule: 'compose/latest-tag', severity: 'warning', message: 'second' },
          { rule: 'compose/no-healthcheck', severity: 'info', message: 'third' },
        ],
        errors: 1,
        warnings: 1,
        infos: 1,
      },
    })

    const texts = wrapper.findAll('li').map(li => li.text())
    expect(texts[0]).toContain('first')
    expect(texts[1]).toContain('second')
    expect(texts[2]).toContain('third')
  })

  it('renders nothing when there is no report and no error yet', () => {
    const wrapper = mountFindings({ report: null })

    expect(wrapper.findAll('li')).toHaveLength(0)
    expect(wrapper.find('.alert').exists()).toBe(false)
  })
})
