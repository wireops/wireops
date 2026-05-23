import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import A11ySkipLink from '../A11ySkipLink.vue'

describe('A11ySkipLink', () => {
  it('links to the main content landmark', () => {
    const wrapper = mount(A11ySkipLink)
    const link = wrapper.get('a')

    expect(link.attributes('href')).toBe('#main-content')
    expect(link.text()).toContain('Skip to main content')
  })
})
