import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import TerminalOutput from '../TerminalOutput.vue'

describe('TerminalOutput', () => {
  it('renders plain lines as text', () => {
    const wrapper = mount(TerminalOutput, {
      props: { lines: ['Pulling image', 'Starting container'] },
    })
    expect(wrapper.text()).toContain('Pulling image')
    expect(wrapper.text()).toContain('Starting container')
  })

  it('applies ANSI color as inline style instead of leaking escape codes', () => {
    const wrapper = mount(TerminalOutput, {
      props: { lines: ['\x1b[32mok\x1b[0m'] },
    })
    expect(wrapper.text()).not.toContain('\x1b')
    expect(wrapper.text()).toContain('ok')
    expect(wrapper.html()).toContain('color: #4ade80')
  })

  it('uses a fixed dark terminal background regardless of app theme', () => {
    const wrapper = mount(TerminalOutput, { props: { lines: ['x'] } })
    expect(wrapper.find('pre').classes()).toContain('bg-carbon-950')
  })

  it('auto-scrolls to the bottom whenever a new line is appended', async () => {
    const wrapper = mount(TerminalOutput, { props: { lines: ['first'] } })
    const el = wrapper.find('pre').element as HTMLElement
    Object.defineProperty(el, 'scrollHeight', { value: 1000, configurable: true })
    el.scrollTop = 0

    await wrapper.setProps({ lines: ['first', 'second'] })
    await nextTick()

    expect(el.scrollTop).toBe(1000)
  })
})
