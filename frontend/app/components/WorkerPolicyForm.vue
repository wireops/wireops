<script setup lang="ts">
const policy = defineModel<{
  inherit: boolean
  allowed_images: string[]
  allowed_volumes: string[]
  allowed_networks: string[]
  allowed_cap_add: string[]
  allowed_devices: string[]
  allowed_security_opt: string[]
  prevent_latest_images: boolean | null
  block_host_volumes: boolean | null
  block_privileged: boolean | null
  block_host_network: boolean | null
  block_host_pid: boolean | null
  block_host_ipc: boolean | null
  block_docker_socket: boolean | null
  allow_render_overrides: boolean | null
}>({ required: true })

const emit = defineEmits<{
  (e: 'save'): void
}>()

// Full set of Linux capabilities Docker supports via --cap-add / cap_add.
const DOCKER_CAPABILITIES = [
  'ALL',
  'AUDIT_CONTROL',
  'AUDIT_READ',
  'AUDIT_WRITE',
  'BLOCK_SUSPEND',
  'BPF',
  'CHECKPOINT_RESTORE',
  'CHOWN',
  'DAC_OVERRIDE',
  'DAC_READ_SEARCH',
  'FOWNER',
  'FSETID',
  'IPC_LOCK',
  'IPC_OWNER',
  'KILL',
  'LEASE',
  'LINUX_IMMUTABLE',
  'MAC_ADMIN',
  'MAC_OVERRIDE',
  'MKNOD',
  'NET_ADMIN',
  'NET_BIND_SERVICE',
  'NET_BROADCAST',
  'NET_RAW',
  'PERFMON',
  'SETFCAP',
  'SETGID',
  'SETPCAP',
  'SETUID',
  'SYS_ADMIN',
  'SYS_BOOT',
  'SYS_CHROOT',
  'SYS_MODULE',
  'SYS_NICE',
  'SYS_PACCT',
  'SYS_PTRACE',
  'SYS_RAWIO',
  'SYS_RESOURCE',
  'SYS_TIME',
  'SYS_TTY_CONFIG',
  'SYSLOG',
  'WAKE_ALARM',
]

function removeCapAdd(cap: string) {
  policy.value.allowed_cap_add = policy.value.allowed_cap_add.filter(c => c !== cap)
  emit('save')
}
</script>

<template>
  <div class="space-y-4">
    <!-- Inherit Global Policy Card -->
    <UCard class="border-primary-500/20 bg-primary-500/5 dark:bg-primary-950/10">
      <div class="flex items-center justify-between">
        <div>
          <p class="text-sm font-medium text-gray-900 dark:text-wire-200 flex items-center gap-2">
            <UIcon name="i-lucide-globe" class="w-4 h-4 text-primary-500 shrink-0" />
            Inherit Global Policy
          </p>
          <p class="text-xs text-gray-400 mt-0.5 ml-6">
            When enabled, this worker uses the global policy. Local overrides are ignored.
          </p>
        </div>
        <USwitch v-model="policy.inherit" @update:model-value="emit('save')" />
      </div>
    </UCard>

    <!-- Policy Overrides -->
    <div :class="{ 'opacity-50 pointer-events-none select-none transition-all': policy.inherit }" class="space-y-4 relative">
      <div v-if="policy.inherit" class="absolute inset-0 z-10" title="Inheriting global policy. Disable inherit to edit."/>

      <!-- Images Policy Card -->
      <UCard>
        <template #header>
          <div class="flex items-center gap-3">
            <div class="flex items-center justify-center w-8 h-8 rounded-lg bg-yellow-400/10 shrink-0">
              <UIcon name="i-lucide-box" class="w-4 h-4 text-yellow-400" />
            </div>
            <div class="min-w-0">
              <h3 class="font-semibold text-gray-900 dark:text-wire-200 text-sm">Images Policy</h3>
              <p class="text-xs text-gray-500 mt-0.5">Control image sources and tags permitted for execution.</p>
            </div>
          </div>
        </template>

        <div class="space-y-6">
          <div class="flex items-center justify-between">
            <div>
              <p class="text-sm font-medium text-gray-900 dark:text-wire-200 flex items-center gap-2">
                <UIcon name="i-lucide-shield-alert" class="w-4 h-4 text-gray-400 shrink-0" />
                Prevent <code>:latest</code> &amp; untagged
              </p>
              <p class="text-xs text-gray-400 mt-0.5 ml-6">
                Blocks images without an explicit version tag (e.g. <code>nginx</code>, <code>nginx:latest</code>).
                Enforced before the image allowlist.
              </p>
            </div>
            <TriStateToggle v-model="policy.prevent_latest_images" @change="emit('save')" />
          </div>

          <USeparator />

          <div class="space-y-3">
            <h4 class="text-sm font-semibold text-gray-900 dark:text-wire-200 flex items-center gap-2">
              <UIcon name="i-lucide-list" class="w-4 h-4 text-gray-400 shrink-0" />
              Allowed Image Patterns
            </h4>
            <PolicyAllowlistEditor
              v-model="policy.allowed_images"
              placeholder="e.g. nginx:* or ghcr.io/org/*"
              empty-text="No restrictions — all images permitted."
              add-label="Add Image Pattern"
              @save="emit('save')"
            />
          </div>
        </div>
      </UCard>

      <!-- Host Volumes Policy Card -->
      <UCard>
        <template #header>
          <div class="flex items-center gap-3">
            <div class="flex items-center justify-center w-8 h-8 rounded-lg bg-yellow-400/10 shrink-0">
              <UIcon name="i-lucide-hard-drive" class="w-4 h-4 text-yellow-400" />
            </div>
            <div class="min-w-0">
              <h3 class="font-semibold text-gray-900 dark:text-wire-200 text-sm">Host Volumes Policy</h3>
              <p class="text-xs text-gray-500 mt-0.5">Restrict host volume directories that can be mounted into containers.</p>
            </div>
          </div>
        </template>

        <div class="space-y-6">
          <div class="flex items-center justify-between">
            <div>
              <p class="text-sm font-medium flex items-center gap-2">
                <UIcon name="i-lucide-ban" class="w-4 h-4 text-gray-400 shrink-0" />
                Block host bind-mounts
              </p>
              <p class="text-xs text-gray-400 mt-0.5 ml-6">
                Prevents mounting host paths (e.g. <code>/data:/data</code>). Only named Docker volumes are allowed.
                Enforced before the volume allowlist.
              </p>
            </div>
            <TriStateToggle v-model="policy.block_host_volumes" @change="emit('save')" />
          </div>

          <USeparator />

          <div class="space-y-3">
            <h4 class="text-sm font-semibold text-gray-900 dark:text-wire-200 flex items-center gap-2">
              <UIcon name="i-lucide-folder-open" class="w-4 h-4 text-gray-400 shrink-0" />
              Allowed Host Volume Paths
            </h4>
            <PolicyAllowlistEditor
              v-model="policy.allowed_volumes"
              placeholder="e.g. /data or myvolume"
              empty-text="No restrictions — all volumes permitted."
              add-label="Add Volume Path"
              @save="emit('save')"
            />
          </div>
        </div>
      </UCard>

      <!-- Docker Networks Policy Card -->
      <UCard>
        <template #header>
          <div class="flex items-center gap-3">
            <div class="flex items-center justify-center w-8 h-8 rounded-lg bg-yellow-400/10 shrink-0">
              <UIcon name="i-lucide-network" class="w-4 h-4 text-yellow-400" />
            </div>
            <div class="min-w-0">
              <h3 class="font-semibold text-gray-900 dark:text-wire-200 text-sm">Docker Networks Policy</h3>
              <p class="text-xs text-gray-500 mt-0.5">Restrict Docker networks that containers can connect to.</p>
            </div>
          </div>
        </template>

        <div class="space-y-3">
          <h4 class="text-sm font-semibold text-gray-900 dark:text-wire-200 flex items-center gap-2">
            <UIcon name="i-lucide-share-2" class="w-4 h-4 text-gray-400 shrink-0" />
            Allowed Docker Networks
          </h4>
          <PolicyAllowlistEditor
            v-model="policy.allowed_networks"
            placeholder="e.g. traefik"
            empty-text="No restrictions — all networks permitted."
            add-label="Add Network"
            @save="emit('save')"
          />
        </div>
      </UCard>

      <!-- Container Isolation Policy Card -->
      <UCard>
        <template #header>
          <div class="flex items-center gap-3">
            <div class="flex items-center justify-center w-8 h-8 rounded-lg bg-red-400/10 shrink-0">
              <UIcon name="i-lucide-shield-alert" class="w-4 h-4 text-red-400" />
            </div>
            <div class="min-w-0">
              <h3 class="font-semibold text-gray-900 dark:text-wire-200 text-sm">Container Isolation Policy</h3>
              <p class="text-xs text-gray-500 mt-0.5">Block Compose options that can grant a container control of the host.</p>
            </div>
          </div>
        </template>

        <div class="space-y-6">
          <div class="flex items-center justify-between">
            <div>
              <p class="text-sm font-medium text-gray-900 dark:text-wire-200 flex items-center gap-2">
                <UIcon name="i-lucide-octagon-alert" class="w-4 h-4 text-gray-400 shrink-0" />
                Block <code>privileged: true</code>
              </p>
              <p class="text-xs text-gray-400 mt-0.5 ml-6">Rejects services that request full host privileges.</p>
            </div>
            <TriStateToggle v-model="policy.block_privileged" @change="emit('save')" />
          </div>

          <USeparator />

          <div class="flex items-center justify-between">
            <div>
              <p class="text-sm font-medium text-gray-900 dark:text-wire-200 flex items-center gap-2">
                <UIcon name="i-lucide-network" class="w-4 h-4 text-gray-400 shrink-0" />
                Block <code>network_mode: host</code>
              </p>
              <p class="text-xs text-gray-400 mt-0.5 ml-6">Rejects services that bypass Docker network isolation.</p>
            </div>
            <TriStateToggle v-model="policy.block_host_network" @change="emit('save')" />
          </div>

          <USeparator />

          <div class="flex items-center justify-between">
            <div>
              <p class="text-sm font-medium text-gray-900 dark:text-wire-200 flex items-center gap-2">
                <UIcon name="i-lucide-cpu" class="w-4 h-4 text-gray-400 shrink-0" />
                Block <code>pid: host</code>
              </p>
              <p class="text-xs text-gray-400 mt-0.5 ml-6">Rejects services that share the host process namespace.</p>
            </div>
            <TriStateToggle v-model="policy.block_host_pid" @change="emit('save')" />
          </div>

          <USeparator />

          <div class="flex items-center justify-between">
            <div>
              <p class="text-sm font-medium text-gray-900 dark:text-wire-200 flex items-center gap-2">
                <UIcon name="i-lucide-share-2" class="w-4 h-4 text-gray-400 shrink-0" />
                Block <code>ipc: host</code>
              </p>
              <p class="text-xs text-gray-400 mt-0.5 ml-6">Rejects services that share the host IPC namespace.</p>
            </div>
            <TriStateToggle v-model="policy.block_host_ipc" @change="emit('save')" />
          </div>

          <USeparator />

          <div class="flex items-center justify-between">
            <div>
              <p class="text-sm font-medium text-gray-900 dark:text-wire-200 flex items-center gap-2">
                <UIcon name="i-lucide-anchor" class="w-4 h-4 text-gray-400 shrink-0" />
                Block Docker socket mount
              </p>
              <p class="text-xs text-gray-400 mt-0.5 ml-6">Rejects mounting <code>/var/run/docker.sock</code> or <code>/run/docker.sock</code> (equivalent to host root).</p>
            </div>
            <TriStateToggle v-model="policy.block_docker_socket" @change="emit('save')" />
          </div>

          <USeparator />

          <div class="space-y-3">
            <h4 class="text-sm font-semibold text-gray-900 dark:text-wire-200 flex items-center gap-2">
              <UIcon name="i-lucide-key-round" class="w-4 h-4 text-gray-400 shrink-0" />
              Allowed Linux Capabilities (<code>cap_add</code>)
            </h4>
            <USelectMenu
              v-model="policy.allowed_cap_add"
              :items="DOCKER_CAPABILITIES"
              multiple
              placeholder="No restrictions — all capabilities permitted."
              class="w-full"
              @update:model-value="emit('save')"
            />
            <div v-if="policy.allowed_cap_add.length" class="flex flex-wrap gap-2">
              <UBadge
                v-for="cap in policy.allowed_cap_add"
                :key="cap"
                color="neutral"
                variant="subtle"
                class="gap-1"
              >
                {{ cap }}
                <UButton
                  icon="i-lucide-x"
                  size="2xs"
                  color="neutral"
                  variant="link"
                  :padded="false"
                  @click="removeCapAdd(cap)"
                />
              </UBadge>
            </div>
          </div>

          <USeparator />

          <div class="space-y-3">
            <h4 class="text-sm font-semibold text-gray-900 dark:text-wire-200 flex items-center gap-2">
              <UIcon name="i-lucide-usb" class="w-4 h-4 text-gray-400 shrink-0" />
              Allowed Host Devices
            </h4>
            <PolicyAllowlistEditor
              v-model="policy.allowed_devices"
              placeholder="e.g. /dev/ttyUSB0"
              empty-text="No restrictions — all devices permitted."
              add-label="Add Device"
              @save="emit('save')"
            />
          </div>

          <USeparator />

          <div class="space-y-3">
            <h4 class="text-sm font-semibold text-gray-900 dark:text-wire-200 flex items-center gap-2">
              <UIcon name="i-lucide-lock-keyhole" class="w-4 h-4 text-gray-400 shrink-0" />
              Allowed <code>security_opt</code> Entries
            </h4>
            <PolicyAllowlistEditor
              v-model="policy.allowed_security_opt"
              placeholder="e.g. no-new-privileges:true"
              empty-text="No restrictions — all security_opt entries permitted."
              add-label="Add Entry"
              @save="emit('save')"
            />
          </div>
        </div>
      </UCard>

      <!-- Render Overrides Policy Card -->
      <UCard>
        <template #header>
          <div class="flex items-center gap-3">
            <div class="flex items-center justify-center w-8 h-8 rounded-lg bg-amber-400/10 shrink-0">
              <UIcon name="i-lucide-sliders-horizontal" class="w-4 h-4 text-amber-400" />
            </div>
            <div class="min-w-0">
              <h3 class="font-semibold text-gray-900 dark:text-wire-200 text-sm">Render Overrides Policy</h3>
              <p class="text-xs text-gray-500 mt-0.5">Controls whether render-time (not committed to git) image/ports/networks overrides can be applied to stacks on this worker.</p>
            </div>
          </div>
        </template>

        <div class="flex items-center justify-between">
          <div>
            <p class="text-sm font-medium text-gray-900 dark:text-wire-200 flex items-center gap-2">
              <UIcon name="i-lucide-sliders-horizontal" class="w-4 h-4 text-gray-400 shrink-0" />
              Allow render-time overrides
            </p>
            <p class="text-xs text-gray-400 mt-0.5 ml-6">
              Blocked by default. When enabled, users can override a service's image, ports, or networks for a
              one-off redeploy without touching Git. Overridden values still pass the allowlists and checks above.
            </p>
          </div>
          <TriStateToggle v-model="policy.allow_render_overrides" @change="emit('save')" />
        </div>
      </UCard>
    </div>
  </div>
</template>
