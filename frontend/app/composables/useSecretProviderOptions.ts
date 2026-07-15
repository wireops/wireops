import vaultIcon from '~/assets/img/icons/integrations/hashicorp-vault.svg'
import infisicalIcon from '~/assets/img/icons/integrations/infisical.svg'

export interface SecretProviderOption {
    label: string
    value: string
    icon?: string
    avatar?: { src: string }
}

// "internal" (AES-GCM at rest) has no backend to enable/disable, so it's
// always offered once the picker is shown. Vault/Infisical only show up
// once their integration is actually enabled. Icons match the ones used on
// the integrations settings page, so a backend reads the same everywhere.
const ALL_PROVIDERS: SecretProviderOption[] = [
    { label: 'Internal', value: 'internal', icon: 'i-lucide-lock' },
    { label: 'Vault', value: 'vault', avatar: { src: vaultIcon } },
    { label: 'Infisical', value: 'infisical', avatar: { src: infisicalIcon } },
]

const EXTERNAL_SLUGS = new Set(['vault', 'infisical'])

// Backend picker is hidden by default (env vars stay "internal"). It only
// appears once at least one external secret backend integration is enabled,
// and then only lists the active ones (+ internal).
export function useSecretProviderOptions() {
    const { getIntegrations } = useIntegrations()
    const activeSlugs = ref<Set<string>>(new Set())
    const loaded = ref(false)

    async function load() {
        try {
            const integrations = await getIntegrations()
            activeSlugs.value = new Set(
                integrations.filter(i => i.enabled && EXTERNAL_SLUGS.has(i.slug)).map(i => i.slug)
            )
        } finally {
            loaded.value = true
        }
    }

    const providerOptions = computed<SecretProviderOption[]>(() => {
        if (!activeSlugs.value.size) return []
        return ALL_PROVIDERS.filter(p => p.value === 'internal' || activeSlugs.value.has(p.value))
    })

    const hasActiveBackends = computed(() => activeSlugs.value.size > 0)

    function iconFor(provider: string) {
        return ALL_PROVIDERS.find(p => p.value === provider)?.icon
    }

    function avatarFor(provider: string) {
        return ALL_PROVIDERS.find(p => p.value === provider)?.avatar
    }

    function labelFor(provider: string) {
        return ALL_PROVIDERS.find(p => p.value === provider)?.label || provider
    }

    return {
        load,
        loaded,
        providerOptions,
        hasActiveBackends,
        iconFor,
        avatarFor,
        labelFor,
    }
}
