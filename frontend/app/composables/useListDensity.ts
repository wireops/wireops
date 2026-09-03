import { computed, ref } from 'vue'

export type ListDensity = 'comfortable' | 'compact'

export const LIST_DENSITY_STORAGE_KEY = 'wireops.listDensity'

export function readStoredListDensity(storage: Pick<Storage, 'getItem'> | null): ListDensity {
  try {
    return storage?.getItem(LIST_DENSITY_STORAGE_KEY) === 'compact' ? 'compact' : 'comfortable'
  } catch {
    return 'comfortable'
  }
}

// localStorage access can throw (Safari private browsing, sandboxed iframes,
// policy-disabled storage) or simply be unavailable rather than just absent -
// guard every access so a blocked/missing store degrades to in-memory-only
// density instead of crashing the toggle.
function getLocalStorage(): Storage | null {
  try {
    return typeof window !== 'undefined' ? window.localStorage : null
  } catch {
    return null
  }
}

// Module-level singleton: every component calling useListDensity() shares
// this one ref, so toggling density in the Stacks panel is instantly
// reflected in the Jobs panel (and vice versa) without a store or prop
// drilling - the two pages share a single "list density" preference.
const density = ref<ListDensity>('comfortable')
let hydrated = false

export function useListDensity() {
  if (!hydrated) {
    density.value = readStoredListDensity(getLocalStorage())
    hydrated = true
  }

  function setDensity(value: ListDensity) {
    density.value = value
    try {
      getLocalStorage()?.setItem(LIST_DENSITY_STORAGE_KEY, value)
    } catch {
      // Storage write blocked - density still applies for the current session.
    }
  }

  function toggleDensity() {
    setDensity(density.value === 'compact' ? 'comfortable' : 'compact')
  }

  return {
    density,
    isCompact: computed(() => density.value === 'compact'),
    setDensity,
    toggleDensity,
  }
}
