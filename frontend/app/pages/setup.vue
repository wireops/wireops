<script setup lang="ts">
import type { SetupStatus } from '~/types/setup'
import { getSetupBlockedMessage, mapSetupError, validateSetupPassword } from '~/utils/setup'
definePageMeta({ layout: false })

const { customGet, customPost } = useApi()
const { login } = useAuth()
const { announce } = useA11yAnnouncer()

const email = ref('')
const password = ref('')
const passwordConfirm = ref('')
const bootstrapToken = ref('')
const showBootstrapToken = ref(false)
const showPassword = ref(false)
const showPasswordConfirm = ref(false)
const loading = ref(false)
const statusLoading = ref(true)
const error = ref('')
const info = ref('')
const setupStatus = ref<SetupStatus | null>(null)

const blockedMessage = computed(() => {
  return getSetupBlockedMessage(setupStatus.value)
})

const setupBlocked = computed(() => {
  return statusLoading.value || !setupStatus.value || setupStatus.value.setupAllowed === false
})

function extractLoginFailureReason(error: any) {
  return (
    error?.response?.message
    || error?.response?.data?.message
    || error?.data?.message
    || error?.message
    || 'Automatic sign-in failed.'
  )
}

async function loadSetupStatus(options?: { preserveError?: boolean }) {
  statusLoading.value = true
  if (!options?.preserveError) {
    error.value = ''
  }
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

function preventConfirmPasswordPaste(event: ClipboardEvent) {
  event.preventDefault()
  announce('Paste is disabled for password confirmation. Type the password again to confirm it.', 'polite')
}

async function handleSetup() {
  error.value = ''
  info.value = ''
  const passwordValidationError = validateSetupPassword(password.value)
  if (passwordValidationError) {
    error.value = passwordValidationError
    announce(error.value, 'assertive')
    return
  }
  if (password.value !== passwordConfirm.value) {
    error.value = mapSetupError('Passwords do not match')
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
    await loadSetupStatus({ preserveError: true })
    return
  }

  try {
    await login(email.value, password.value)
    announce('Administrator account created')
    await navigateTo('/')
  } catch (e: any) {
    error.value = extractLoginFailureReason(e)
    info.value = 'Administrator created successfully. Automatic sign-in failed, so please sign in manually.'
    announce(error.value, 'assertive')
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
        <div class="flex items-center justify-center w-12 h-12 rounded-xl bg-yellow-400/10 border border-yellow-400/20 mb-3 shadow-[0_0_24px_rgba(255,198,0,0.15)] sm:w-16 sm:h-16 sm:rounded-2xl sm:mb-4">
          <UIcon name="i-lucide-zap" class="w-7 h-7 text-yellow-400 drop-shadow-[0_0_8px_rgba(255,198,0,0.7)] sm:w-9 sm:h-9" />
        </div>
        <h1 class="text-2xl font-black tracking-[0.22em] uppercase text-yellow-400 drop-shadow-[0_0_12px_rgba(255,198,0,0.4)] sm:text-3xl sm:tracking-widest">
          wireops
        </h1>
        <p class="text-sm text-wire-400 mt-1 tracking-wide">Initial Setup</p>
      </div>

      <!-- Setup card -->
      <div class="rounded-2xl border border-carbon-800 bg-carbon-900 p-6 shadow-2xl">
        <div class="mb-5 rounded-2xl border border-wire-400/20 bg-wire-400/10 p-4 text-center shadow-[0_0_24px_rgba(93,168,255,0.08)]">
          <p class="text-xs font-semibold uppercase tracking-[0.24em] text-wire-300">
            Welcome to Wireops
          </p>
          <p class="mt-2 text-sm text-gray-300">
            Finish the first-time setup to create your administrator account.
          </p>
        </div>

        <form class="flex flex-col gap-4" @submit.prevent="handleSetup">
          <UAlert v-if="blockedMessage" color="warning" :title="blockedMessage" icon="i-lucide-shield-alert" />
          <UAlert v-if="error" color="error" :title="error" icon="i-lucide-alert-circle" role="alert" aria-live="assertive" />
          <UAlert v-if="info" color="info" :title="info" icon="i-lucide-info">
            <template #description>
              <NuxtLink to="/login" class="text-sm underline underline-offset-4">
                Continue to the sign-in page
              </NuxtLink>
            </template>
          </UAlert>

          <UFormField v-if="setupStatus?.requiresBootstrapToken">
            <template #label>
              <span class="flex items-center gap-1.5">
                Bootstrap Token
                <UTooltip text="You can find this value in the BOOTSTRAP_TOKEN environment variable.">
                  <UIcon name="i-lucide-info" class="w-3.5 h-3.5 text-gray-400 cursor-help" />
                </UTooltip>
              </span>
            </template>
            <UInput
              v-model="bootstrapToken"
              :type="showBootstrapToken ? 'text' : 'password'"
              size="xl"
              class="w-full"
              :ui="{ base: 'text-sm px-3 py-2.5 ps-10', leadingIcon: 'size-4' }"
              placeholder="Enter the bootstrap token"
              icon="i-lucide-key-round"
              required
              aria-label="Bootstrap token"
              :disabled="setupBlocked || loading"
            >
              <template #trailing>
                <UButton
                  type="button"
                  color="neutral"
                  variant="link"
                  size="xs"
                  :icon="showBootstrapToken ? 'i-lucide-eye-off' : 'i-lucide-eye'"
                  :aria-label="showBootstrapToken ? 'Hide bootstrap token' : 'Show bootstrap token'"
                  :disabled="setupBlocked || loading"
                  @click="showBootstrapToken = !showBootstrapToken"
                />
              </template>
            </UInput>
          </UFormField>

          <p v-if="setupStatus?.requiresBootstrapToken" class="text-xs text-gray-500 -mt-2">
            This token is only used during the initial setup.
          </p>

          <UFormField label="Email">
            <UInput
              v-model="email"
              type="email"
              size="xl"
              class="w-full"
              :ui="{ base: 'text-sm px-3 py-2.5 ps-10', leadingIcon: 'size-4' }"
              placeholder="admin@example.com"
              icon="i-lucide-mail"
              required
              aria-label="Email"
              :disabled="setupBlocked || loading"
            />
          </UFormField>

          <UFormField label="Password">
            <UInput
              v-model="password"
              :type="showPassword ? 'text' : 'password'"
              size="xl"
              class="w-full"
              :ui="{ base: 'text-sm px-3 py-2.5 ps-10', leadingIcon: 'size-4' }"
              placeholder="••••••••"
              icon="i-lucide-lock"
              required
              aria-label="Password"
              :disabled="setupBlocked || loading"
            >
              <template #trailing>
                <UButton
                  type="button"
                  color="neutral"
                  variant="link"
                  size="xs"
                  :icon="showPassword ? 'i-lucide-eye-off' : 'i-lucide-eye'"
                  :aria-label="showPassword ? 'Hide password' : 'Show password'"
                  :disabled="setupBlocked || loading"
                  @click="showPassword = !showPassword"
                />
              </template>
            </UInput>
          </UFormField>

          <p class="text-xs text-gray-500 -mt-2">
            Use at least 8 characters for the administrator password.
          </p>

          <UFormField label="Confirm Password">
            <UInput
              v-model="passwordConfirm"
              :type="showPasswordConfirm ? 'text' : 'password'"
              size="xl"
              class="w-full"
              :ui="{ base: 'text-sm px-3 py-2.5 ps-10', leadingIcon: 'size-4' }"
              placeholder="••••••••"
              icon="i-lucide-lock"
              required
              aria-label="Confirm password"
              :disabled="setupBlocked || loading"
              @paste="preventConfirmPasswordPaste"
            >
              <template #trailing>
                <UButton
                  type="button"
                  color="neutral"
                  variant="link"
                  size="xs"
                  :icon="showPasswordConfirm ? 'i-lucide-eye-off' : 'i-lucide-eye'"
                  :aria-label="showPasswordConfirm ? 'Hide password confirmation' : 'Show password confirmation'"
                  :disabled="setupBlocked || loading"
                  @click="showPasswordConfirm = !showPasswordConfirm"
                />
              </template>
            </UInput>
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

          <p v-if="setupStatus?.reason === 'already_configured'" class="text-center text-sm text-gray-400">
            <NuxtLink to="/login" class="text-yellow-400 hover:text-yellow-300 transition-colors">
              Sign in to the existing administrator account
            </NuxtLink>
          </p>
        </form>
      </div>
    </div>
  </main>
</template>
