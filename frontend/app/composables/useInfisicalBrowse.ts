export interface InfisicalEnvironment {
    name: string
    slug: string
}

export interface InfisicalProject {
    id: string
    name: string
    environments: InfisicalEnvironment[]
}

export interface InfisicalBrowseEntry {
    name: string
    is_folder: boolean
}

// Read-only Infisical project/environment/secret browsing, used by
// InfisicalReferencePicker to build a
// "<project-id>/<environment>/<secret-path>#<SECRET_NAME>" reference without
// hand-typing it. Connection config (site_url/client_id/client_secret) itself
// is managed via useIntegrations() (slug "infisical"), alongside every other
// integration.
//
// Whether a project-scoped machine identity can list every org project
// varies by Infisical version/instance — some versions require broader
// org-level permission than a project-scoped identity has and 403 there.
// listInfisicalProjects is tried opportunistically by the picker; if it
// fails, getInfisicalProject (single project by ID, always within a
// project-scoped identity's grant) is the fallback.
export function useInfisicalBrowse() {
    const { customGet } = useApi()

    async function listInfisicalProjects() {
        return customGet<InfisicalProject[]>('/api/custom/integrations/infisical/projects')
    }

    async function getInfisicalProject(projectId: string) {
        return customGet<InfisicalProject>(`/api/custom/integrations/infisical/project?project_id=${encodeURIComponent(projectId)}`)
    }

    async function browseInfisicalPath(projectId: string, environment: string, path: string) {
        const params = new URLSearchParams({ project_id: projectId, environment, path })
        return customGet<InfisicalBrowseEntry[]>(`/api/custom/integrations/infisical/browse?${params.toString()}`)
    }

    return {
        listInfisicalProjects,
        getInfisicalProject,
        browseInfisicalPath,
    }
}
