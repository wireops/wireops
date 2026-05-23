<script setup lang="ts">
import { watch } from 'vue'

definePageMeta({ layout: false })

const { login, getSSOProviders, loginWithSSO } = useAuth()
const { announce } = useA11yAnnouncer()

const email = ref('')
const password = ref('')
const loading = ref(false)
const ssoLoading = ref(false)
const error = ref('')

const ssoProviders = ref<{ name: string; displayName: string }[]>([])

onMounted(async () => {
  try {
    ssoProviders.value = await getSSOProviders()
  } catch (e) {
    console.warn('Failed to load SSO providers', e)
    ssoProviders.value = []
  }
})

async function handleLogin() {
  loading.value = true
  error.value = ''
  try {
    await login(email.value, password.value)
    announce('Login successful')
    navigateTo('/')
  } catch (e: any) {
    error.value = e?.message || 'Login failed'
    announce(error.value, 'assertive')
  } finally {
    loading.value = false
  }
}

async function handleSSOLogin(providerName: string) {
  ssoLoading.value = true
  error.value = ''
  try {
    await loginWithSSO(providerName)
    announce(`Continuing with ${providerName}`)
    navigateTo('/')
  } catch (e: any) {
    error.value = e?.message || 'SSO login failed'
    announce(error.value, 'assertive')
  } finally {
    ssoLoading.value = false
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
        <p class="text-sm text-wire-400 mt-1 tracking-wide">GitOps Orchestrator</p>
      </div>

      <!-- Login card -->
      <div class="rounded-2xl border border-carbon-800 bg-carbon-900 p-6 shadow-2xl">
        <form class="flex flex-col gap-4" @submit.prevent="handleLogin">
          <UAlert v-if="error" color="error" :title="error" icon="i-lucide-alert-circle" role="alert" aria-live="assertive" />

          <UFormField label="Email">
            <UInput
              v-model="email"
              type="email"
              placeholder="admin@example.com"
              icon="i-lucide-mail"
              required
              class="w-full"
              aria-label="Email"
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
            />
          </UFormField>

          <UButton
            type="submit"
            block
            :loading="loading"
            icon="i-lucide-zap"
            label="Sign In"
            class="mt-2"
          />
          <div class="text-center mt-1">
            <NuxtLink to="/forgot-password" class="text-xs text-gray-500 hover:text-yellow-400 transition-colors">
              Forgot your password?
            </NuxtLink>
          </div>
        </form>

        <template v-if="ssoProviders.length > 0">
          <div class="flex items-center gap-3 mt-4">
            <div class="flex-1 h-px bg-carbon-700" />
            <span class="text-xs text-wire-500">or</span>
            <div class="flex-1 h-px bg-carbon-700" />
          </div>

          <div class="flex flex-col gap-2 mt-3">
            <UButton
              v-for="provider in ssoProviders"
              :key="provider.name"
              block
              variant="outline"
              :loading="ssoLoading"
              icon="i-lucide-shield"
              :label="`Continue with ${provider.displayName}`"
              class="border-carbon-700 text-wire-300 hover:border-yellow-400/50 hover:text-yellow-400"
              @click="handleSSOLogin(provider.name)"
            />
          </div>
        </template>
      </div>
    </div>
  </main>
</template>
