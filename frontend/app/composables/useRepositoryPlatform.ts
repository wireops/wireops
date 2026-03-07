const CDN_BASE = 'https://cdn.jsdelivr.net/gh/selfhst/icons/svg'

const SUPPORTED_PLATFORMS = new Set(['github', 'gitlab', 'gitea', 'forgejo', 'bitbucket'])

export const PLATFORM_OPTIONS = [
    { label: 'GitHub', value: 'github' },
    { label: 'GitLab', value: 'gitlab' },
    { label: 'Gitea', value: 'gitea' },
    { label: 'Forgejo', value: 'forgejo' },
    { label: 'Bitbucket', value: 'bitbucket' },
]

export function platformIconUrl(platform: string | undefined | null): string | null {
    if (!platform || !SUPPORTED_PLATFORMS.has(platform)) return null
    return `${CDN_BASE}/${platform}.svg`
}

export function useRepositoryPlatform() {
    return { PLATFORM_OPTIONS, platformIconUrl }
}
