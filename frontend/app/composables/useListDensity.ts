import { computed, ref } from 'vue'

export type ListDensity = 'comfortable' | 'compact'

export const LIST_DENSITY_STORAGE_KEY = 'wireops.listDensity'

export function readStoredListDensity(storage: Pick<Storage, 'getItem'> | null): ListDensity {
  return storage?.getItem(LIST_DENSITY_STORAGE_KEY) === 'compact' ? 'compact' : 'comfortable'
}

// Module-level singleton: every component calling useListDensity() shares
// this one ref, so toggling density in the Stacks panel is instantly
// reflected in the Jobs panel (and vice versa) without a store or prop
// drilling - the two pages share a single "list density" preference.
const density = ref<ListDensity>('comfortable')
let hydrated = false

export function useListDensity() {
  if (!hydrated && typeof window !== 'undefined') {
    density.value = readStoredListDensity(window.localStorage)
    hydrated = true
  }

  function setDensity(value: ListDensity) {
    density.value = value
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(LIST_DENSITY_STORAGE_KEY, value)
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
