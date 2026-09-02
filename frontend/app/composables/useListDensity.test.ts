import { afterEach, describe, expect, it } from 'vitest'
import { readStoredListDensity, useListDensity } from './useListDensity'

// Newer Node versions ship their own experimental global `localStorage`
// getter/setter on globalThis, which some Node/happy-dom/vitest combinations
// let win over the test environment's own Storage - installing a plain
// object via defineProperty (bypassing that setter) keeps this test
// deterministic across Node versions instead of depending on whichever
// localStorage the ambient test environment happened to wire up.
function installFakeLocalStorage(): Storage {
  const store = new Map<string, string>()
  const fakeStorage = {
    getItem: (key: string) => store.get(key) ?? null,
    setItem: (key: string, value: string) => { store.set(key, value) },
    removeItem: (key: string) => { store.delete(key) },
    clear: () => { store.clear() },
  } as Storage
  Object.defineProperty(window, 'localStorage', { value: fakeStorage, configurable: true })
  return fakeStorage
}

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
    const fakeStorage = installFakeLocalStorage()
    const { setDensity } = useListDensity()
    setDensity('compact')

    expect(fakeStorage.getItem('wireops.listDensity')).toBe('compact')

    setDensity('comfortable')
    expect(fakeStorage.getItem('wireops.listDensity')).toBe('comfortable')
  })

  it('toggleDensity flips back and forth', () => {
    const { density, toggleDensity } = useListDensity()

    toggleDensity()
    expect(density.value).toBe('compact')

    toggleDensity()
    expect(density.value).toBe('comfortable')
  })
})
