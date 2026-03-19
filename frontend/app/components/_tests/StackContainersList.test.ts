import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import StackContainersList from '../StackContainersList.vue'

describe('StackContainersList', () => {
    const stubs = {
        UTooltip: { template: '<div><slot /></div>' },
        UIcon: { 
            template: '<span class="u-icon" :name="name"></span>',
            props: ['name']
        }
    }

    it('should URL-encode slugs in icon URLs', () => {
        const containers = [
            { name: 'container1', is_fallback: false, slug: 'test/slug' }
        ]
        const wrapper = mount(StackContainersList, {
            props: { containers },
            global: { stubs }
        })
        const img = wrapper.find('img')
        expect(img.attributes('src')).toContain('test%2Fslug')
    })

    it('should use fallback icon when slug is missing', () => {
        const containers = [
            { name: 'container1', is_fallback: true }
        ]
        const wrapper = mount(StackContainersList, {
            props: { containers },
            global: { stubs }
        })
        const icon = wrapper.find('.u-icon')
        expect(icon.attributes('name')).toBe('i-lucide-box')
    })

    it('should transition to fallback icon on image error', async () => {
        const containers = [
            { name: 'container1', is_fallback: false, slug: 'fail-slug' }
        ]
        const wrapper = mount(StackContainersList, {
            props: { containers },
            global: { stubs }
        })
        
        const img = wrapper.find('img')
        expect(img.exists()).toBe(true)
        
        // Trigger error
        await img.trigger('error')
        
        // Img should be gone, fallback icon should appear
        expect(wrapper.find('img').exists()).toBe(false)
        const icon = wrapper.find('.u-icon')
        expect(icon.exists()).toBe(true)
        expect(icon.attributes('name')).toBe('i-lucide-box')
    })
})
