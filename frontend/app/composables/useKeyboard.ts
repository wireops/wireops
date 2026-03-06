export function useKeyboard() {
  const router = useRouter()
  const isShowingHelp = ref(false)

  const shortcuts = [
    { key: 'Cmd/Ctrl + K', description: 'Quick search (coming soon)' },
    { key: 'Cmd/Ctrl + R', description: 'Refresh current page' },
    { key: 'Cmd/Ctrl + S', description: 'Trigger sync (on stack page)' },
    { key: 'Escape', description: 'Close modals' },
    { key: 'G then D', description: 'Go to Dashboard' },
    { key: 'G then S', description: 'Go to Stacks' },
    { key: 'G then R', description: 'Go to Repositories' },
    { key: '?', description: 'Show this help' },
  ]

  let gPressed = false
  let gTimeout: NodeJS.Timeout | null = null

  function handleKeydown(event: KeyboardEvent) {
    const target = event.target as HTMLElement
    const isInput = target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable

    // ? - Show help (not in input)
    if (event.key === '?' && !isInput && !event.ctrlKey && !event.metaKey) {
      event.preventDefault()
      isShowingHelp.value = !isShowingHelp.value
      return
    }

    // Escape - Close help modal
    if (event.key === 'Escape') {
      if (isShowingHelp.value) {
        event.preventDefault()
        isShowingHelp.value = false
        return
      }
      // Let other components handle escape (for closing their modals)
      return
    }

    // Cmd/Ctrl + K - Quick search (placeholder for now)
    if ((event.metaKey || event.ctrlKey) && event.key === 'k') {
      event.preventDefault()
      // TODO: Implement global search when ready
      return
    }

    // Cmd/Ctrl + R - Refresh (let browser handle, but we could add custom logic)
    if ((event.metaKey || event.ctrlKey) && event.key === 'r') {
      // Let browser handle default refresh
      return
    }

    // Cmd/Ctrl + S - Trigger sync (only on stack page, handled by page component)
    if ((event.metaKey || event.ctrlKey) && event.key === 's') {
      // Handled by stack detail page
      return
    }

    // G then X navigation (not in inputs)
    if (!isInput) {
      if (event.key === 'g' || event.key === 'G') {
        gPressed = true
        if (gTimeout) clearTimeout(gTimeout)
        gTimeout = setTimeout(() => {
          gPressed = false
        }, 1000)
        return
      }

      if (gPressed) {
        gPressed = false
        if (gTimeout) clearTimeout(gTimeout)

        switch (event.key.toLowerCase()) {
          case 'd':
            event.preventDefault()
            router.push('/')
            break
          case 's':
            event.preventDefault()
            router.push('/stacks')
            break
          case 'r':
            event.preventDefault()
            router.push('/repositories')
            break
        }
      }
    }
  }

  onMounted(() => {
    window.addEventListener('keydown', handleKeydown)
  })

  onUnmounted(() => {
    window.removeEventListener('keydown', handleKeydown)
    if (gTimeout) clearTimeout(gTimeout)
  })

  return {
    isShowingHelp,
    shortcuts,
  }
}
