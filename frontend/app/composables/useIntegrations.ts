import { ref } from 'vue'

export interface IntegrationAction {
    integration_slug: string
    kind: 'reverse-proxy' | 'log' | 'secret'
    label: string
    url: string
    icon?: string
}

export interface Integration {
    slug: string
    name: string
    category: string
    enabled: boolean
    config: Record<string, any>
}

export function useIntegrations() {
    const { customGet, customPut, customDelete } = useApi()

    async function getIntegrations() {
        return customGet<Integration[]>('/api/custom/integrations')
    }

    async function saveIntegration(slug: string, enabled: boolean, config: Record<string, any>) {
        return customPut<{ slug: string; enabled: boolean; config: Record<string, any> }>(
            `/api/custom/integrations/${slug}`,
            { enabled, config }
        )
    }

    async function deleteIntegration(slug: string) {
        return customDelete<{ status: string }>(`/api/custom/integrations/${slug}`)
    }

    async function getStackIntegrationActions(stackId: string) {
        return customGet<Record<string, IntegrationAction[]>>(
            `/api/custom/stacks/${stackId}/integration-actions`
        )
    }

    return {
        getIntegrations,
        saveIntegration,
        deleteIntegration,
        getStackIntegrationActions,
    }
}
