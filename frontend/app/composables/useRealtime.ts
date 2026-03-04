export function useRealtime() {
  const { $pb } = useNuxtApp()
  const subscriptions = new Map<string, () => void>()

  /**
   * Subscribe to realtime updates for a collection
   * @param collection - Collection name to subscribe to
   * @param callback - Callback to execute on updates
   * @param filter - Optional filter expression
   */
  async function subscribe(
    collection: string,
    callback: (data: any) => void,
    filter?: string
  ) {
    const key = `${collection}:${filter || '*'}`
    
    // Unsubscribe if already subscribed
    if (subscriptions.has(key)) {
      unsubscribe(key)
    }

    // Subscribe to collection changes - this returns a Promise
    try {
      const unsubscribeFn = await $pb.collection(collection).subscribe('*', (e) => {
        callback(e)
      }, filter ? { filter } : undefined)

      subscriptions.set(key, unsubscribeFn)
      
      return () => unsubscribe(key)
    } catch (error) {
      console.error('Failed to subscribe to collection:', collection, error)
      return () => {}
    }
  }

  /**
   * Unsubscribe from a specific subscription
   */
  function unsubscribe(key: string) {
    const unsubscribeFn = subscriptions.get(key)
    if (unsubscribeFn) {
      try {
        unsubscribeFn()
      } catch (error) {
        console.error('Failed to unsubscribe:', error)
      }
      subscriptions.delete(key)
    }
  }

  /**
   * Unsubscribe from all active subscriptions
   */
  function unsubscribeAll() {
    subscriptions.forEach((unsubscribeFn) => {
      try {
        unsubscribeFn()
      } catch (error) {
        console.error('Failed to unsubscribe:', error)
      }
    })
    subscriptions.clear()
  }

  // Cleanup on component unmount
  onUnmounted(() => {
    unsubscribeAll()
  })

  return {
    subscribe,
    unsubscribe,
    unsubscribeAll,
  }
}
