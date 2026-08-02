import { describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import { usePaginatedList } from './usePaginatedList'

function makeFetcher(all: { id: number }[]) {
  return vi.fn(async ({ page, perPage }: { page: number, perPage: number }) => {
    const start = (page - 1) * perPage
    return {
      items: all.slice(start, start + perPage),
      totalItems: all.length,
    }
  })
}

describe('usePaginatedList', () => {
  it('loads the first page on init', async () => {
    const all = Array.from({ length: 5 }, (_, i) => ({ id: i }))
    const fetcher = makeFetcher(all)

    const list = usePaginatedList(fetcher, { perPage: 2 })
    await nextTick()
    await nextTick()

    expect(fetcher).toHaveBeenCalledTimes(1)
    expect(list.items.value).toEqual([{ id: 0 }, { id: 1 }])
    expect(list.totalItems.value).toBe(5)
    expect(list.totalPages.value).toBe(3)
  })

  it('refetches when page changes', async () => {
    const all = Array.from({ length: 5 }, (_, i) => ({ id: i }))
    const fetcher = makeFetcher(all)

    const list = usePaginatedList(fetcher, { perPage: 2 })
    await nextTick()
    await nextTick()

    list.page.value = 2
    await nextTick()
    await nextTick()

    expect(fetcher).toHaveBeenCalledTimes(2)
    expect(list.items.value).toEqual([{ id: 2 }, { id: 3 }])
  })

  it('clamps the current page when it runs past the new total after a shrink', async () => {
    const all = Array.from({ length: 3 }, (_, i) => ({ id: i }))
    const fetcher = makeFetcher(all)

    const list = usePaginatedList(fetcher, { perPage: 2 })
    await nextTick()
    await nextTick()

    list.page.value = 2
    await nextTick()
    await nextTick()
    expect(list.items.value).toEqual([{ id: 2 }])

    // shrink the backing data out from under page 2
    all.pop()
    list.reload()
    await nextTick()
    await nextTick()

    expect(list.page.value).toBe(1)
  })

  it('debounces watched filter changes into a single reload on page 1', async () => {
    vi.useFakeTimers()
    try {
      const all = Array.from({ length: 5 }, (_, i) => ({ id: i }))
      const fetcher = makeFetcher(all)
      const search = ref('')

      const list = usePaginatedList(fetcher, { perPage: 2, watchDebounced: [search], debounceMs: 300 })
      await vi.advanceTimersByTimeAsync(0)
      expect(fetcher).toHaveBeenCalledTimes(1)

      list.page.value = 2
      await vi.advanceTimersByTimeAsync(0)
      expect(fetcher).toHaveBeenCalledTimes(2)

      search.value = 'a'
      await vi.advanceTimersByTimeAsync(0)
      search.value = 'ab'
      await vi.advanceTimersByTimeAsync(0)
      search.value = 'abc'
      await vi.advanceTimersByTimeAsync(300)

      // only one extra fetch for the settled search value, and it reset to page 1
      expect(fetcher).toHaveBeenCalledTimes(3)
      expect(list.page.value).toBe(1)
    } finally {
      vi.useRealTimers()
    }
  })
})
