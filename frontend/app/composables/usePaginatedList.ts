import { ref, watch, type Ref } from 'vue'

export const DEFAULT_PAGE_SIZE = 24

export type PaginatedFetcher<T> = (params: {
  page: number
  perPage: number
  sort: string
}) => Promise<{ items: T[], totalItems: number }>

/**
 * Reactive page/perPage/sort state wrapping a paginated fetcher (PocketBase
 * getList or a custom paginated endpoint). Callers build their own `filter`
 * computed from search/status/group refs and feed it into the fetcher they
 * pass in - this composable only owns paging/sorting/debounce/loading state,
 * not filter-string construction (that's collection/endpoint-specific).
 *
 * Realtime updates are handled by refetching the current page rather than
 * patching individual items in place - reconciling inserts/deletes across
 * page boundaries isn't worth the complexity for the lists this wraps today.
 */
export function usePaginatedList<T>(
  fetcher: PaginatedFetcher<T>,
  options: {
    perPage?: number
    sort?: string | Ref<string>
    /** Anything reactive that should trigger a debounced refetch (search text, filter selects). */
    watchDebounced?: unknown[]
    debounceMs?: number
  } = {}
) {
  const page = ref(1)
  const perPage = ref(options.perPage ?? DEFAULT_PAGE_SIZE)
  const sort = typeof options.sort === 'object' ? options.sort : ref(options.sort ?? '-updated')

  const items = ref<T[]>([]) as Ref<T[]>
  const totalItems = ref(0)
  const totalPages = ref(0)
  const loading = ref(false)
  const isRefreshing = ref(false)

  let requestId = 0

  async function load(background = false) {
    const thisRequest = ++requestId
    if (background) isRefreshing.value = true
    else loading.value = true

    try {
      const result = await fetcher({ page: page.value, perPage: perPage.value, sort: sort.value })
      if (thisRequest !== requestId) return // a newer request superseded this one
      items.value = result.items
      totalItems.value = result.totalItems
      totalPages.value = Math.max(1, Math.ceil(result.totalItems / perPage.value))
      if (page.value > totalPages.value) {
        page.value = totalPages.value
      }
    } finally {
      if (thisRequest === requestId) {
        loading.value = false
        isRefreshing.value = false
      }
    }
  }

  let debounceTimer: ReturnType<typeof setTimeout> | undefined
  function reload(background = false) {
    clearTimeout(debounceTimer)
    load(background)
  }

  // Resetting `page` to 1 from the debounced-filter handler below would also
  // fire the [page, perPage, sort] watcher and double-fetch; this flag lets
  // that one programmatic reset skip its own reload since the debounced
  // handler already triggers exactly one load itself.
  let suppressNextPageWatch = false

  watch([page, perPage, sort], () => {
    if (suppressNextPageWatch) {
      suppressNextPageWatch = false
      return
    }
    reload(false)
  })

  if (options.watchDebounced?.length) {
    watch(options.watchDebounced, () => {
      clearTimeout(debounceTimer)
      debounceTimer = setTimeout(() => {
        if (page.value !== 1) {
          suppressNextPageWatch = true
          page.value = 1
        }
        load(false)
      }, options.debounceMs ?? 400)
    })
  }

  load(false)

  return {
    page,
    perPage,
    sort,
    items,
    totalItems,
    totalPages,
    loading,
    isRefreshing,
    /** Refetch the current page - pass true for a background refresh (e.g. from a realtime event) that doesn't toggle the main loading spinner. */
    reload,
  }
}
