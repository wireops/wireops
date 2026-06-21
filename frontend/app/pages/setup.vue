<script setup lang="ts">
import type { SetupStatus } from '~/types/setup'
definePageMeta({ layout: false })


const { customGet, customPost } = useApi()
const { login } = useAuth()
const { announce } = useA11yAnnouncer()

const email = ref('')
const password = ref('')
const passwordConfirm = ref('')
const bootstrapToken = ref('')
const loading = ref(false)
const statusLoading = ref(true)
const error = ref('')
const info = ref('')
const setupStatus = ref<SetupStatus | null>(null)

const blockedMessage = computed(() => {
  if (setupStatus.value?.reason === 'missing_bootstrap_token') {
    return 'Initial setup is blocked because BOOTSTRAP_TOKEN is not configured on the server.'
  }
  if (setupStatus.value?.reason === 'already_configured') {
    return 'Setup has already been completed for this Wireops instance.'
  }
  return ''
})

const setupBlocked = computed(() => {
  return statusLoading.value || !setupStatus.value || setupStatus.value.setupAllowed === false
})

function mapSetupError(message?: string) {
  switch (message) {
    case 'invalid email address':
      return 'Enter a valid email address.'
    case 'password must be at least 8 characters':
      return 'Password must be at least 8 characters long.'
    case 'invalid bootstrap token':
      return 'The bootstrap token is invalid. Check the server configuration and try again.'
    case 'bootstrap token is not configured':
      return 'BOOTSTRAP_TOKEN is not configured on the server.'
    case 'setup has already been completed':
      return 'Setup has already been completed. Sign in with the existing administrator account.'
    default:
      return message || 'Setup failed'
  }
}

async function loadSetupStatus() {
  statusLoading.value = true
  error.value = ''
  try {
    setupStatus.value = await customGet<SetupStatus>('/api/custom/setup/status')
  } catch (e: any) {
    setupStatus.value = null
    error.value = 'Unable to check setup status right now. Refresh and try again.'
    announce(error.value, 'assertive')
  } finally {
    statusLoading.value = false
  }
}

await loadSetupStatus()

async function handleSetup() {
  error.value = ''
  info.value = ''
  if (password.value !== passwordConfirm.value) {
    error.value = 'Passwords do not match'
    announce(error.value, 'assertive')
    return
  }
  if (setupBlocked.value) {
    error.value = blockedMessage.value || 'Setup is not available right now.'
    announce(error.value, 'assertive')
    return
  }
  loading.value = true
  try {
    await customPost('/api/custom/setup', {
      email: email.value,
      password: password.value,
      bootstrapToken: bootstrapToken.value,
    })
  } catch (e: any) {
    error.value = mapSetupError(e?.message)
    announce(error.value, 'assertive')
    loading.value = false
    await loadSetupStatus()
    return
  }

  try {
    await login(email.value, password.value)
    announce('Administrator account created')
    await navigateTo('/')
  } catch (e: any) {
    info.value = 'Administrator created successfully. Automatic sign-in failed, so please sign in manually.'
    announce(info.value, 'assertive')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <main id="main-content" tabindex="-1" class="min-h-screen flex items-center justify-center bg-carbon-950 relative overflow-hidden">
    <!-- Decorative lightning grid -->
    <div class="absolute inset-0 pointer-events-none select-none opacity-5">
      <svg width="100%" height="100%" xmlns="http://www.w3.org/2000/svg">
        <defs>
          <pattern id="grid" width="60" height="60" patternUnits="userSpaceOnUse">
            <path d="M 60 0 L 0 0 0 60" fill="none" stroke="#5da8ff" stroke-width="0.5"/>
          </pattern>
        </defs>
        <rect width="100%" height="100%" fill="url(#grid)" />
      </svg>
    </div>

    <!-- Ambient glow -->
    <div class="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-96 h-96 bg-yellow-400/5 rounded-full blur-3xl pointer-events-none" />
    <div class="absolute top-1/3 left-1/3 w-64 h-64 bg-wire-400/5 rounded-full blur-3xl pointer-events-none" />

    <div class="relative z-10 w-full max-w-sm px-4">
      <!-- Logo / Brand -->
      <div class="flex flex-col items-center mb-8">
        <div class="flex items-center justify-center w-16 h-16 rounded-2xl bg-yellow-400/10 border border-yellow-400/20 mb-4 shadow-[0_0_24px_rgba(255,198,0,0.15)]">
          <UIcon name="i-lucide-zap" class="w-9 h-9 text-yellow-400 drop-shadow-[0_0_8px_rgba(255,198,0,0.7)]" />
        </div>
        <h1 class="text-3xl font-black tracking-widest uppercase text-yellow-400 drop-shadow-[0_0_12px_rgba(255,198,0,0.4)]">
          wireops
        </h1>
        <p class="text-sm text-wire-400 mt-1 tracking-wide">Initial Setup</p>
      </div>

      <!-- Setup card -->
      <div class="rounded-2xl border border-carbon-800 bg-carbon-900 p-6 shadow-2xl">
        <p class="text-sm text-gray-400 mb-5 text-center">
          Create the first administrator account to get started.
        </p>

        <form class="flex flex-col gap-4" @submit.prevent="handleSetup">
          <UAlert v-if="blockedMessage" color="warning" :title="blockedMessage" icon="i-lucide-shield-alert" />
          <UAlert v-if="error" color="error" :title="error" icon="i-lucide-alert-circle" role="alert" aria-live="assertive" />
          <UAlert v-if="info" color="info" :title="info" icon="i-lucide-info" />

          <UFormField v-if="setupStatus?.requiresBootstrapToken" label="Bootstrap Token">
            <UInput
              v-model="bootstrapToken"
              type="password"
              placeholder="Enter the bootstrap token"
              icon="i-lucide-key-round"
              required
              class="w-full"
              aria-label="Bootstrap token"
              :disabled="setupBlocked || loading"
            />
          </UFormField>

          <UFormField label="Email">
            <UInput
              v-model="email"
              type="email"
              placeholder="admin@example.com"
              icon="i-lucide-mail"
              required
              class="w-full"
              aria-label="Email"
              :disabled="setupBlocked || loading"
            />
          </UFormField>

          <UFormField label="Password">
            <UInput
              v-model="password"
              type="password"
              placeholder="••••••••"
              icon="i-lucide-lock"
              required
              class="w-full"
              aria-label="Password"
              :disabled="setupBlocked || loading"
            />
          </UFormField>

          <p class="text-xs text-gray-500 -mt-2">
            Use at least 8 characters for the administrator password.
          </p>

          <UFormField label="Confirm Password">
            <UInput
              v-model="passwordConfirm"
              type="password"
              placeholder="••••••••"
              icon="i-lucide-lock"
              required
              class="w-full"
              aria-label="Confirm password"
              :disabled="setupBlocked || loading"
            />
          </UFormField>

          <UButton
            type="submit"
            block
            :loading="loading || statusLoading"
            icon="i-lucide-shield-check"
            label="Create Administrator"
            class="mt-2"
            :disabled="setupBlocked || loading"
          />
        </form>
      </div>
    </div>
  </main>
</template>
