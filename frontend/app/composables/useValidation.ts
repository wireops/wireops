export function useValidation() {
  function validateComposePath(path: string): string | null {
    if (!path || path === '.') return null
    if (path.includes('..')) return 'Path must not contain ".."'
    if (path.startsWith('/')) return 'Path must be relative'
    return null
  }

  function validateComposeFile(file: string): string | null {
    if (!file) return null
    if (file.includes('..')) return 'Filename must not contain ".."'
    if (file.startsWith('/')) return 'Filename must be relative'
    if (file.includes('/') || file.includes('\\')) return 'Must be a filename, not a path'
    const ext = file.split('.').pop()?.toLowerCase()
    if (ext !== 'yml' && ext !== 'yaml') return 'Must end in .yml or .yaml'
    return null
  }

  return { validateComposePath, validateComposeFile }
}
