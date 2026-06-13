type Role = 'viewer' | 'operator' | 'admin'

const roleRank: Record<Role, number> = {
  viewer: 1,
  operator: 2,
  admin: 3,
}

function normalizeRole(role?: string): Role {
  if (role === 'operator' || role === 'admin') return role
  return 'viewer'
}

export function usePermissions() {
  const { $pb } = useNuxtApp()
  const role = computed<Role>(() => normalizeRole($pb.authStore.record?.role))
  const atLeast = (minimum: Role) => roleRank[role.value] >= roleRank[minimum]

  return {
    role,
    isViewer: computed(() => role.value === 'viewer'),
    isOperator: computed(() => atLeast('operator')),
    isAdmin: computed(() => atLeast('admin')),
    canOperate: computed(() => atLeast('operator')),
    canManageRepos: computed(() => atLeast('operator')),
    canManageSettings: computed(() => atLeast('admin')),
  }
}
