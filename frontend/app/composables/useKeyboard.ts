import { onMounted, onUnmounted, ref } from 'vue'

const isShowingHelp = ref(false)
const isShowingCommandPalette = ref(false)

export function useKeyboard() {
  const router = useRouter()

  const shortcuts = [
    { key: 'Cmd/Ctrl + K', description: 'Quick search (Command Palette)' },
    { key: 'Cmd/Ctrl + R', description: 'Refresh current page' },
    { key: 'Cmd/Ctrl + S', description: 'Trigger sync (on stack page)' },
    { key: 'Escape', description: 'Close modals' },
    { key: 'G then D', description: 'Go to Dashboard' },
    { key: 'G then W', description: 'Go to Stacks' },
    { key: 'G then R', description: 'Go to Repositories' },
    { key: 'G then K', description: 'Go to Repository Keys' },
    { key: '1 / 2', description: 'Switch repository tabs (on Repositories page)' },
    { key: '/', description: 'Focus page search (on list pages)' },
    { key: 'R', description: 'Refresh current repositories tab' },
    { key: 'N', description: 'Create item on current repositories tab' },
    { key: '?', description: 'Show this help' },
  ]

  let gPressed = false
  let gTimeout: NodeJS.Timeout | null = null

  function handleKeydown(event: KeyboardEvent) {
    const target = event.target as HTMLElement
    const tagName = target?.tagName?.toUpperCase()
    const role = target?.getAttribute('role')
    const isInput = tagName === 'INPUT'
      || tagName === 'TEXTAREA'
      || tagName === 'SELECT'
      || target?.isContentEditable
      || role === 'textbox'
      || role === 'combobox'
      || role === 'listbox'
      || role === 'menu'
      || !!target?.closest('[contenteditable="true"]')

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
      if (isShowingCommandPalette.value) {
        event.preventDefault()
        isShowingCommandPalette.value = false
        return
      }
      // Let other components handle escape (for closing their modals)
      return
    }

    // Cmd/Ctrl + K - Quick search
    if ((event.metaKey || event.ctrlKey) && event.key === 'k') {
      event.preventDefault()
      isShowingCommandPalette.value = !isShowingCommandPalette.value
      return
    }

    // Cmd/Ctrl + R - Refresh (let browser handle)
    if ((event.metaKey || event.ctrlKey) && event.key === 'r') {
      return
    }

    // Cmd/Ctrl + S - Trigger sync
    if ((event.metaKey || event.ctrlKey) && event.key === 's') {
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
          case 'w':
            event.preventDefault()
            router.push('/stacks')
            break
          case 'r':
            event.preventDefault()
            router.push('/repositories')
            break
          case 'k':
            event.preventDefault()
            router.push('/repositories?tab=keys')
            break
        }
      }
    }
  }

  onMounted(() => {
    if (typeof window !== 'undefined' && !window.__keyboardListenerRegistered) {
      window.addEventListener('keydown', handleKeydown)
      window.__keyboardListenerRegistered = true
    }
  })

  onUnmounted(() => {
    if (gTimeout) clearTimeout(gTimeout)
  })

  return {
    isShowingHelp,
    isShowingCommandPalette,
    shortcuts,
  }
}
