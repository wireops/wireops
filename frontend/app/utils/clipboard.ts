/**
 * Copy text to clipboard
 * @param text - Text to copy
 * @returns Promise that resolves when text is copied
 */
export async function copyToClipboard(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
      return true
    } else {
      // Fallback for older browsers or non-secure contexts
      const textArea = document.createElement('textarea')
      textArea.value = text
      textArea.style.position = 'fixed'
      textArea.style.left = '-999999px'
      textArea.style.top = '-999999px'
      document.body.appendChild(textArea)
      textArea.focus()
      textArea.select()
      const successful = document.execCommand('copy')
      textArea.remove()
      return successful
    }
  } catch (err) {
    console.error('Failed to copy text:', err)
    return false
  }
}

/**
 * Composable for copy operations with toast notifications
 */
export function useCopy() {
  const toast = useToast()

  async function copy(text: string, label: string = 'Text') {
    const success = await copyToClipboard(text)
    if (success) {
      toast.add({ 
        title: `${label} copied!`, 
        color: 'success',
        icon: 'i-lucide-check'
      })
    } else {
      toast.add({ 
        title: 'Failed to copy', 
        color: 'error',
        icon: 'i-lucide-x'
      })
    }
    return success
  }

  return { copy }
}
