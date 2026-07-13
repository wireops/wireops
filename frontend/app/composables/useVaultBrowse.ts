export interface VaultMount {
    path: string
    version: string
}

export interface VaultBrowseEntry {
    name: string
    is_folder: boolean
}

// Read-only Vault mount/secret browsing, used by VaultReferencePicker to
// build a "mount/data/path#field" reference without hand-typing it.
// Connection config (address/token) itself is managed via useIntegrations()
// (slug "vault"), alongside every other integration.
export function useVaultBrowse() {
    const { customGet } = useApi()

    async function listVaultMounts() {
        return customGet<VaultMount[]>('/api/custom/integrations/vault/mounts')
    }

    async function browseVaultPath(mount: string, path: string, version: string) {
        const params = new URLSearchParams({ mount, path, version })
        return customGet<VaultBrowseEntry[]>(`/api/custom/integrations/vault/browse?${params.toString()}`)
    }

    async function listVaultFields(mount: string, path: string, version: string) {
        const params = new URLSearchParams({ mount, path, version })
        return customGet<string[]>(`/api/custom/integrations/vault/fields?${params.toString()}`)
    }

    return {
        listVaultMounts,
        browseVaultPath,
        listVaultFields,
    }
}
