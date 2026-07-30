import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import ComposePreview from '../ComposePreview.vue'

// Stubs use setup() + h() rather than template HTML strings — Codacy flags
// inline HTML-string stubs as XSS even in test-only code.
const stubs = {
  UIcon: {
    props: ['name'],
    setup(props: { name?: string }) {
      return () => h('span', { class: 'icon', 'data-name': props.name })
    },
  },
  UTooltip: {
    props: ['text'],
    setup(props: { text?: string }, { slots }: any) {
      return () => h('span', { class: 'tooltip', 'data-text': props.text }, slots.default?.())
    },
  },
}

const SOURCE = [
  'services:',
  '  web:',
  '    image: nginx:latest',
  '    ports:',
  '      - "8080:80"',
].join('\n')

function mountPreview(props: Record<string, unknown> = {}) {
  return mount(ComposePreview, {
    props: { content: SOURCE, filename: 'docker-compose.yml', findings: [], ...props },
    global: { stubs },
  })
}

describe('ComposePreview', () => {
  // Unconditional, so a failing assertion inside a fake-timer test cannot
  // leak them into the tests that follow.
  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders every line with a 1-based line number', () => {
    const wrapper = mountPreview()

    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(5)
    expect(rows[0]!.find('td').text()).toBe('1')
    expect(rows[4]!.find('td').text()).toBe('5')
    expect(rows[2]!.text()).toContain('image: nginx:latest')
  })

  it('marks only the lines that carry a finding', () => {
    const wrapper = mountPreview({
      findings: [{ rule: 'compose/latest-tag', severity: 'warning', line: 3, message: 'unpinned image' }],
    })

    const rows = wrapper.findAll('tbody tr')
    expect(rows[2]!.classes().join(' ')).toContain('amber')
    expect(rows[0]!.classes().join(' ')).not.toContain('amber')
    // One marker icon, on the affected line only.
    expect(wrapper.findAll('.tooltip')).toHaveLength(1)
  })

  it('shows a warning triangle for errors and warnings', () => {
    const wrapper = mountPreview({
      findings: [
        { rule: 'policy/images', severity: 'error', line: 3, message: 'blocked image' },
        { rule: 'compose/no-restart-policy', severity: 'warning', line: 2, message: 'no restart policy' },
      ],
    })

    const icons = wrapper.findAll('.tooltip .icon').map(i => i.attributes('data-name'))
    expect(icons).toHaveLength(2)
    expect(icons.every(name => name === 'i-lucide-triangle-alert')).toBe(true)
  })

  it('escalates a line carrying both a warning and an error to error styling', () => {
    const wrapper = mountPreview({
      findings: [
        { rule: 'compose/latest-tag', severity: 'warning', line: 3, message: 'unpinned' },
        { rule: 'policy/images', severity: 'error', line: 3, message: 'blocked' },
      ],
    })

    const row = wrapper.findAll('tbody tr')[2]!
    expect(row.classes().join(' ')).toContain('red')
    expect(row.classes().join(' ')).not.toContain('amber')
  })

  it('lists every message for a line in its tooltip', () => {
    const wrapper = mountPreview({
      findings: [
        { rule: 'a', severity: 'error', line: 3, message: 'first problem' },
        { rule: 'b', severity: 'warning', line: 3, message: 'second problem' },
      ],
    })

    const tooltip = wrapper.find('.tooltip').attributes('data-text')
    expect(tooltip).toContain('first problem')
    expect(tooltip).toContain('second problem')
  })

  it('emits select-line when a marked line is clicked', async () => {
    const wrapper = mountPreview({
      findings: [{ rule: 'a', severity: 'error', line: 3, message: 'problem' }],
    })

    await wrapper.findAll('tbody tr')[2]!.find('td').trigger('click')
    expect(wrapper.emitted('select-line')).toEqual([[3]])
  })

  it('emits select-line on Enter and Space for a marked line', async () => {
    const wrapper = mountPreview({
      findings: [{ rule: 'a', severity: 'error', line: 3, message: 'problem' }],
    })

    const gutter = wrapper.findAll('tbody tr')[2]!.find('td')
    // Exposed as a button so it is reachable and operable by keyboard, not
    // just by pointer.
    expect(gutter.attributes('role')).toBe('button')
    expect(gutter.attributes('tabindex')).toBe('0')

    await gutter.trigger('keydown.enter')
    await gutter.trigger('keydown.space')
    expect(wrapper.emitted('select-line')).toEqual([[3], [3]])
  })

  it('leaves unmarked lines out of the keyboard tab order', async () => {
    const wrapper = mountPreview({
      findings: [{ rule: 'a', severity: 'error', line: 3, message: 'problem' }],
    })

    const gutter = wrapper.findAll('tbody tr')[0]!.find('td')
    expect(gutter.attributes('role')).toBeUndefined()
    expect(gutter.attributes('tabindex')).toBeUndefined()

    await gutter.trigger('keydown.enter')
    await gutter.trigger('keydown.space')
    expect(wrapper.emitted('select-line')).toBeUndefined()
  })

  it('does not emit select-line for an unmarked line', async () => {
    const wrapper = mountPreview({
      findings: [{ rule: 'a', severity: 'error', line: 3, message: 'problem' }],
    })

    await wrapper.findAll('tbody tr')[0]!.find('td').trigger('click')
    expect(wrapper.emitted('select-line')).toBeUndefined()
  })

  it('reports findings that could not be tied to a line', () => {
    const wrapper = mountPreview({
      findings: [
        { rule: 'a', severity: 'error', line: 3, message: 'located' },
        { rule: 'b', severity: 'warning', message: 'unlocated' },
      ],
    })

    expect(wrapper.text()).toContain('1 finding could not be tied to a specific line')
  })

  it('renders file content as text, never as markup', () => {
    const wrapper = mountPreview({
      content: 'services:\n  web:\n    image: "<img src=x onerror=alert(1)>"',
      findings: [],
    })

    // The payload must survive as visible text and produce no element.
    expect(wrapper.text()).toContain('<img src=x onerror=alert(1)>')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.html()).not.toContain('<img src=x')
  })

  it('normalises CRLF so no stray carriage returns are rendered', () => {
    const wrapper = mountPreview({ content: 'services:\r\n  web:\r\n', findings: [] })

    const rows = wrapper.findAll('tbody tr')
    expect(rows[0]!.text()).not.toContain('\r')
    expect(rows[1]!.text()).not.toContain('\r')
    // Leading indentation is part of the code and must survive. Read
    // textContent directly — Vue Test Utils' .text() trims it away.
    expect(rows[1]!.find('td:last-child').element.textContent).toBe('  web:')
  })

  it('focusOn flashes the requested line and can be called repeatedly', async () => {
    vi.useFakeTimers()
    const wrapper = mountPreview({
      findings: [{ rule: 'a', severity: 'error', line: 3, message: 'problem' }],
    })

    ;(wrapper.vm as any).focusOn(3)
    await wrapper.vm.$nextTick()
    expect(wrapper.findAll('tbody tr')[2]!.classes().join(' ')).toContain('ring')

    vi.advanceTimersByTime(1500)
    await wrapper.vm.$nextTick()
    expect(wrapper.findAll('tbody tr')[2]!.classes().join(' ')).not.toContain('ring')

    // Selecting the same line again must flash it again.
    ;(wrapper.vm as any).focusOn(3)
    await wrapper.vm.$nextTick()
    expect(wrapper.findAll('tbody tr')[2]!.classes().join(' ')).toContain('ring')
  })
})
