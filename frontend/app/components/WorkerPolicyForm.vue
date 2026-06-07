<script setup lang="ts">
import { ref, nextTick } from 'vue'

const policy = defineModel<{
  allowed_images: string[]
  allowed_volumes: string[]
  allowed_networks: string[]
  prevent_latest_images: boolean
  block_host_volumes: boolean
}>({ required: true })

const emit = defineEmits<{
  (e: 'save'): void
}>()

const originalValues = ref<Record<string, string | undefined>>({})

const activeEditImageIndex = ref<number | null>(null)
const activeEditVolumeIndex = ref<number | null>(null)
const activeEditNetworkIndex = ref<number | null>(null)

const imagesContainer = ref<HTMLElement | null>(null)
const volumesContainer = ref<HTMLElement | null>(null)
const networksContainer = ref<HTMLElement | null>(null)

function unlockImage(index: number, event?: Event) {
  activeEditImageIndex.value = index
  if (event) {
    nextTick(() => {
      const target = event.currentTarget as HTMLElement
      const parent = target.closest('.flex')
      const input = parent?.querySelector('input') as HTMLInputElement | null
      input?.focus()
    })
  }
}

function lockImage(index: number, event?: Event) {
  activeEditImageIndex.value = null
  if (event) {
    const target = event.currentTarget as HTMLElement
    const parent = target.closest('.flex')
    const input = parent?.querySelector('input') as HTMLInputElement | null
    input?.blur()
  }
}

function unlockVolume(index: number, event?: Event) {
  activeEditVolumeIndex.value = index
  if (event) {
    nextTick(() => {
      const target = event.currentTarget as HTMLElement
      const parent = target.closest('.flex')
      const input = parent?.querySelector('input') as HTMLInputElement | null
      input?.focus()
    })
  }
}

function lockVolume(index: number, event?: Event) {
  activeEditVolumeIndex.value = null
  if (event) {
    const target = event.currentTarget as HTMLElement
    const parent = target.closest('.flex')
    const input = parent?.querySelector('input') as HTMLInputElement | null
    input?.blur()
  }
}

function unlockNetwork(index: number, event?: Event) {
  activeEditNetworkIndex.value = index
  if (event) {
    nextTick(() => {
      const target = event.currentTarget as HTMLElement
      const parent = target.closest('.flex')
      const input = parent?.querySelector('input') as HTMLInputElement | null
      input?.focus()
    })
  }
}

function lockNetwork(index: number, event?: Event) {
  activeEditNetworkIndex.value = null
  if (event) {
    const target = event.currentTarget as HTMLElement
    const parent = target.closest('.flex')
    const input = parent?.querySelector('input') as HTMLInputElement | null
    input?.blur()
  }
}

function addAllowedImage() {
  const len = policy.value.allowed_images.length
  if (len > 0 && !policy.value.allowed_images[len - 1].trim()) {
    activeEditImageIndex.value = len - 1
    nextTick(() => {
      const inputs = imagesContainer.value?.querySelectorAll('input')
      const lastInput = inputs?.[inputs.length - 1] as HTMLInputElement | null
      lastInput?.focus()
    })
    return
  }
  policy.value.allowed_images.push('')
  activeEditImageIndex.value = policy.value.allowed_images.length - 1
  nextTick(() => {
    const inputs = imagesContainer.value?.querySelectorAll('input')
    const lastInput = inputs?.[inputs.length - 1] as HTMLInputElement | null
    lastInput?.focus()
  })
}

function addAllowedVolume() {
  const len = policy.value.allowed_volumes.length
  if (len > 0 && !policy.value.allowed_volumes[len - 1].trim()) {
    activeEditVolumeIndex.value = len - 1
    nextTick(() => {
      const inputs = volumesContainer.value?.querySelectorAll('input')
      const lastInput = inputs?.[inputs.length - 1] as HTMLInputElement | null
      lastInput?.focus()
    })
    return
  }
  policy.value.allowed_volumes.push('')
  activeEditVolumeIndex.value = policy.value.allowed_volumes.length - 1
  nextTick(() => {
    const inputs = volumesContainer.value?.querySelectorAll('input')
    const lastInput = inputs?.[inputs.length - 1] as HTMLInputElement | null
    lastInput?.focus()
  })
}

function addAllowedNetwork() {
  const len = policy.value.allowed_networks.length
  if (len > 0 && !policy.value.allowed_networks[len - 1].trim()) {
    activeEditNetworkIndex.value = len - 1
    nextTick(() => {
      const inputs = networksContainer.value?.querySelectorAll('input')
      const lastInput = inputs?.[inputs.length - 1] as HTMLInputElement | null
      lastInput?.focus()
    })
    return
  }
  policy.value.allowed_networks.push('')
  activeEditNetworkIndex.value = policy.value.allowed_networks.length - 1
  nextTick(() => {
    const inputs = networksContainer.value?.querySelectorAll('input')
    const lastInput = inputs?.[inputs.length - 1] as HTMLInputElement | null
    lastInput?.focus()
  })
}

function onFocusInput(val: string, type: 'image' | 'volume' | 'network', index: number) {
  const key = `${type}-${index}`
  originalValues.value[key] = val
  if (type === 'image') activeEditImageIndex.value = index
  if (type === 'volume') activeEditVolumeIndex.value = index
  if (type === 'network') activeEditNetworkIndex.value = index
}

function onBlurInput(val: string, type: 'image' | 'volume' | 'network', index: number) {
  if (type === 'image') activeEditImageIndex.value = null
  if (type === 'volume') activeEditVolumeIndex.value = null
  if (type === 'network') activeEditNetworkIndex.value = null

  const key = `${type}-${index}`
  const origVal = originalValues.value[key] ?? ''

  setTimeout(() => {
    let list: string[] = []
    if (type === 'image') list = policy.value.allowed_images
    else if (type === 'volume') list = policy.value.allowed_volumes
    else if (type === 'network') list = policy.value.allowed_networks

    if (list && index < list.length && typeof list[index] === 'string' && !list[index].trim()) {
      if (origVal.trim() === '') {
        list.splice(index, 1)
      } else {
        list[index] = origVal
      }
    } else if (val !== origVal && list && index < list.length && typeof list[index] === 'string') {
      emit('save')
    }
    originalValues.value[key] = undefined
  }, 150)
}

const showDeleteRuleModal = ref(false)
const deleteRuleContext = ref<{
  type: 'image' | 'volume' | 'network'
  index: number
  value: string
} | null>(null)

function requestDeleteRule(type: 'image' | 'volume' | 'network', index: number, value: string) {
  if (!value.trim()) {
    executeDeleteRule(type, index)
    return
  }
  deleteRuleContext.value = { type, index, value }
  showDeleteRuleModal.value = true
}

function executeDeleteRule(type: 'image' | 'volume' | 'network', index: number) {
  if (type === 'image') {
    policy.value.allowed_images.splice(index, 1)
  } else if (type === 'volume') {
    policy.value.allowed_volumes.splice(index, 1)
  } else if (type === 'network') {
    policy.value.allowed_networks.splice(index, 1)
  }
  emit('save')
  showDeleteRuleModal.value = false
  deleteRuleContext.value = null
}
</script>

<template>
  <div class="space-y-4">
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
          <USwitch v-model="policy.prevent_latest_images" @update:model-value="emit('save')" />
        </div>

        <USeparator />

        <div class="space-y-3">
          <div>
            <h4 class="text-sm font-semibold text-gray-900 dark:text-wire-200 flex items-center gap-2">
              <UIcon name="i-lucide-list" class="w-4 h-4 text-gray-400 shrink-0" />
              Allowed Image Patterns
            </h4>
          </div>

          <div ref="imagesContainer" class="space-y-2">
            <div
              v-for="(_, i) in policy.allowed_images"
              :key="i"
              class="flex items-center gap-2"
            >
              <UInput
                v-model="policy.allowed_images[i]"
                placeholder="e.g. nginx:* or ghcr.io/org/*"
                class="flex-1 font-mono text-sm"
                :readonly="activeEditImageIndex !== i"
                :ui="{ base: 'pl-11 pr-11', leading: 'pointer-events-auto h-full pl-0', trailing: 'pointer-events-auto h-full pr-0' }"
                @click="unlockImage(i)"
                @focus="onFocusInput(policy.allowed_images[i], 'image', i)"
                @blur="onBlurInput(policy.allowed_images[i], 'image', i)"
                @keyup.enter="($event.target as any).blur()"
              >
                <template #leading>
                  <div class="flex items-center h-[calc(100%-4px)] bg-white dark:bg-carbon-950 px-2.5 border-r border-gray-200 dark:border-carbon-800 rounded-l-[4px] shrink-0 ml-[2px] my-[2px]">
                    <UButton
                      :icon="activeEditImageIndex === i ? 'i-lucide-lock-open' : 'i-lucide-lock'"
                      variant="ghost"
                      :color="activeEditImageIndex === i ? 'primary' : 'neutral'"
                      size="xs"
                      :class="['p-0.5 hover:bg-gray-50 dark:hover:bg-carbon-900 bg-transparent rounded transition-opacity', activeEditImageIndex === i ? 'opacity-100' : 'opacity-50']"
                      :aria-label="activeEditImageIndex === i ? 'Lock' : 'Unlock'"
                      tabindex="-1"
                      @click.stop="activeEditImageIndex === i ? lockImage(i, $event) : unlockImage(i, $event)"
                    />
                  </div>
                </template>
                <template #trailing>
                  <UButton
                    icon="i-lucide-x"
                    variant="ghost"
                    color="error"
                    class="h-[calc(100%-4px)] flex items-center justify-center bg-white dark:bg-carbon-950 px-4 border-l border-gray-200 dark:border-carbon-800 rounded-r-[4px] shrink-0 mr-[2px] my-[2px] text-red-500 hover:!bg-red-500 hover:!text-white focus:!bg-red-500 focus:!text-white focus-visible:!bg-red-500 focus-visible:!text-white focus:!outline-none focus-visible:!outline-none transition-colors"
                    :aria-label="'Delete image pattern'"
                    @click.stop="requestDeleteRule('image', i, policy.allowed_images[i])"
                    @keydown.enter.prevent.stop="requestDeleteRule('image', i, policy.allowed_images[i])"
                    @keyup.enter.prevent.stop
                  />
                </template>
              </UInput>
            </div>
            <div v-if="policy.allowed_images.length === 0" class="text-xs text-gray-400 italic ml-6">No restrictions — all images permitted.</div>
          </div>
          
          <div class="ml-6">
            <UButton icon="i-lucide-plus" size="xs" variant="outline" label="Add Image Pattern" @click="addAllowedImage" />
          </div>
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
          <USwitch v-model="policy.block_host_volumes" @update:model-value="emit('save')" />
        </div>

        <USeparator />

        <div class="space-y-3">
          <div>
            <h4 class="text-sm font-semibold text-gray-900 dark:text-wire-200 flex items-center gap-2">
              <UIcon name="i-lucide-folder-open" class="w-4 h-4 text-gray-400 shrink-0" />
              Allowed Host Volume Paths
            </h4>
          </div>

          <div ref="volumesContainer" class="space-y-2">
            <div
              v-for="(_, i) in policy.allowed_volumes"
              :key="i"
              class="flex items-center gap-2"
            >
              <UInput
                v-model="policy.allowed_volumes[i]"
                placeholder="e.g. /data or myvolume"
                class="flex-1 font-mono text-sm"
                :readonly="activeEditVolumeIndex !== i"
                :ui="{ base: 'pl-11 pr-11', leading: 'pointer-events-auto h-full pl-0', trailing: 'pointer-events-auto h-full pr-0' }"
                @click="unlockVolume(i)"
                @focus="onFocusInput(policy.allowed_volumes[i], 'volume', i)"
                @blur="onBlurInput(policy.allowed_volumes[i], 'volume', i)"
                @keyup.enter="($event.target as any).blur()"
              >
                <template #leading>
                  <div class="flex items-center h-[calc(100%-4px)] bg-white dark:bg-carbon-950 px-2.5 border-r border-gray-200 dark:border-carbon-800 rounded-l-[4px] shrink-0 ml-[2px] my-[2px]">
                    <UButton
                      :icon="activeEditVolumeIndex === i ? 'i-lucide-lock-open' : 'i-lucide-lock'"
                      variant="ghost"
                      :color="activeEditVolumeIndex === i ? 'primary' : 'neutral'"
                      size="xs"
                      :class="['p-0.5 hover:bg-gray-50 dark:hover:bg-carbon-900 bg-transparent rounded transition-opacity', activeEditVolumeIndex === i ? 'opacity-100' : 'opacity-50']"
                      :aria-label="activeEditVolumeIndex === i ? 'Lock' : 'Unlock'"
                      tabindex="-1"
                      @click.stop="activeEditVolumeIndex === i ? lockVolume(i, $event) : unlockVolume(i, $event)"
                    />
                  </div>
                </template>
                <template #trailing>
                  <UButton
                    icon="i-lucide-x"
                    variant="ghost"
                    color="error"
                    class="h-[calc(100%-4px)] flex items-center justify-center bg-white dark:bg-carbon-950 px-4 border-l border-gray-200 dark:border-carbon-800 rounded-r-[4px] shrink-0 mr-[2px] my-[2px] text-red-500 hover:!bg-red-500 hover:!text-white focus:!bg-red-500 focus:!text-white focus-visible:!bg-red-500 focus-visible:!text-white focus:!outline-none focus-visible:!outline-none transition-colors"
                    :aria-label="'Delete volume path'"
                    @click.stop="requestDeleteRule('volume', i, policy.allowed_volumes[i])"
                    @keydown.enter.prevent.stop="requestDeleteRule('volume', i, policy.allowed_volumes[i])"
                    @keyup.enter.prevent.stop
                  />
                </template>
              </UInput>
            </div>
            <div v-if="policy.allowed_volumes.length === 0" class="text-xs text-gray-400 italic ml-6">No restrictions — all volumes permitted.</div>
          </div>

          <div class="ml-6">
            <UButton icon="i-lucide-plus" size="xs" variant="outline" label="Add Volume Path" @click="addAllowedVolume" />
          </div>
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

      <div class="space-y-6">
        <div class="space-y-3">
          <div class="flex items-center justify-between">
            <div>
              <p class="text-sm font-medium text-gray-900 dark:text-wire-200 flex items-center gap-2">
                <UIcon name="i-lucide-share-2" class="w-4 h-4 text-gray-400 shrink-0" />
                Allowed Docker Networks
              </p>
            </div>
            <UButton icon="i-lucide-plus" size="xs" variant="outline" label="Add" @click="addAllowedNetwork" />
          </div>

          <div ref="networksContainer" class="space-y-2">
            <div
              v-for="(_, i) in policy.allowed_networks"
              :key="i"
              class="flex items-center gap-2"
            >
              <UInput
                v-model="policy.allowed_networks[i]"
                placeholder="e.g. traefik"
                class="flex-1 font-mono text-sm"
                :readonly="activeEditNetworkIndex !== i"
                :ui="{ base: 'pl-11 pr-11', leading: 'pointer-events-auto h-full pl-0', trailing: 'pointer-events-auto h-full pr-0' }"
                @click="unlockNetwork(i)"
                @focus="onFocusInput(policy.allowed_networks[i], 'network', i)"
                @blur="onBlurInput(policy.allowed_networks[i], 'network', i)"
                @keyup.enter="($event.target as any).blur()"
              >
                <template #leading>
                  <div class="flex items-center h-[calc(100%-4px)] bg-white dark:bg-carbon-950 px-2.5 border-r border-gray-200 dark:border-carbon-800 rounded-l-[4px] shrink-0 ml-[2px] my-[2px]">
                    <UButton
                      :icon="activeEditNetworkIndex === i ? 'i-lucide-lock-open' : 'i-lucide-lock'"
                      variant="ghost"
                      :color="activeEditNetworkIndex === i ? 'primary' : 'neutral'"
                      size="xs"
                      :class="['p-0.5 hover:bg-gray-50 dark:hover:bg-carbon-900 bg-transparent rounded transition-opacity', activeEditNetworkIndex === i ? 'opacity-100' : 'opacity-50']"
                      :aria-label="activeEditNetworkIndex === i ? 'Lock' : 'Unlock'"
                      tabindex="-1"
                      @click.stop="activeEditNetworkIndex === i ? lockNetwork(i, $event) : unlockNetwork(i, $event)"
                    />
                  </div>
                </template>
                <template #trailing>
                  <UButton
                    icon="i-lucide-x"
                    variant="ghost"
                    color="error"
                    class="h-[calc(100%-4px)] flex items-center justify-center bg-white dark:bg-carbon-950 px-4 border-l border-gray-200 dark:border-carbon-800 rounded-r-[4px] shrink-0 mr-[2px] my-[2px] text-red-500 hover:!bg-red-500 hover:!text-white focus:!bg-red-500 focus:!text-white focus-visible:!bg-red-500 focus-visible:!text-white focus:!outline-none focus-visible:!outline-none transition-colors"
                    :aria-label="'Delete network'"
                    @click.stop="requestDeleteRule('network', i, policy.allowed_networks[i])"
                    @keydown.enter.prevent.stop="requestDeleteRule('network', i, policy.allowed_networks[i])"
                    @keyup.enter.prevent.stop
                  />
                </template>
              </UInput>
            </div>
            <div v-if="policy.allowed_networks.length === 0" class="text-xs text-gray-400 italic ml-6">No restrictions — all networks permitted.</div>
          </div>
        </div>
      </div>
    </UCard>

    <!-- Delete Rule Confirmation Modal -->
    <UModal v-if="showDeleteRuleModal" v-model:open="showDeleteRuleModal">
      <template #content>
        <DeleteRuleModal
          :value="deleteRuleContext?.value || ''"
          @confirm="executeDeleteRule(deleteRuleContext!.type, deleteRuleContext!.index)"
          @cancel="showDeleteRuleModal = false"
        />
      </template>
    </UModal>
  </div>
</template>
