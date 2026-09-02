import { afterEach, describe, expect, it } from 'vitest'
import { readStoredListDensity, useListDensity } from './useListDensity'

describe('readStoredListDensity', () => {
  it('defaults to comfortable when storage is null', () => {
    expect(readStoredListDensity(null)).toBe('comfortable')
  })

  it('defaults to comfortable when the stored value is not recognized', () => {
    expect(readStoredListDensity({ getItem: () => 'bogus' })).toBe('comfortable')
  })

  it('reads compact when explicitly stored', () => {
    expect(readStoredListDensity({ getItem: () => 'compact' })).toBe('compact')
  })
})

describe('useListDensity', () => {
  afterEach(() => {
    useListDensity().setDensity('comfortable')
  })

  it('shares density state across separate calls (singleton)', () => {
    const a = useListDensity()
    const b = useListDensity()

    expect(a.isCompact.value).toBe(false)
    a.toggleDensity()

    expect(a.density.value).toBe('compact')
    expect(b.density.value).toBe('compact')
    expect(b.isCompact.value).toBe(true)
  })

  it('persists the chosen density to localStorage', () => {
    const { setDensity } = useListDensity()
    setDensity('compact')

    expect(window.localStorage.getItem('wireops.listDensity')).toBe('compact')

    setDensity('comfortable')
    expect(window.localStorage.getItem('wireops.listDensity')).toBe('comfortable')
  })

  it('toggleDensity flips back and forth', () => {
    const { density, toggleDensity } = useListDensity()

    toggleDensity()
    expect(density.value).toBe('compact')

    toggleDensity()
    expect(density.value).toBe('comfortable')
  })
})
