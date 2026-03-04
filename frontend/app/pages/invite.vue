<script setup lang="ts">
definePageMeta({ layout: false })

const { $pb } = useNuxtApp()
const route = useRoute()

const token = computed(() => (route.query.token as string) || '')

type PageState = 'loading' | 'valid' | 'invalid' | 'success'
const state = ref<PageState>('loading')
const email = ref('')
const password = ref('')
const passwordConfirm = ref('')
const loading = ref(false)
const error = ref('')
const stateError = ref('')

onMounted(async () => {
  if (!token.value) {
    stateError.value = 'No invite token found in the URL.'
    state.value = 'invalid'
    return
  }
  try {
    const res = await fetch(`${$pb.baseURL}/api/custom/users/invite/validate?token=${token.value}`)
    const data = await res.json()
    if (!res.ok) {
      stateError.value = data.error || 'This invite link is invalid or has expired.'
      state.value = 'invalid'
      return
    }
    email.value = data.email
    state.value = 'valid'
  } catch {
    stateError.value = 'Could not validate the invite. Please try again.'
    state.value = 'invalid'
  }
})

async function handleSubmit() {
  if (password.value !== passwordConfirm.value) {
    error.value = 'Passwords do not match.'
    return
  }
  loading.value = true
  error.value = ''
  try {
    const res = await fetch(`${$pb.baseURL}/api/custom/users/invite/accept`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        token: token.value,
        password: password.value,
        password_confirm: passwordConfirm.value,
      }),
    })
    const data = await res.json()
    if (!res.ok) throw new Error(data.error || 'Failed to create account.')
    state.value = 'success'
    setTimeout(() => navigateTo('/login'), 3000)
  } catch (e: any) {
    error.value = e?.message || 'Something went wrong. Please try again.'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="min-h-screen flex items-center justify-center bg-carbon-950 relative overflow-hidden">
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
    <div class="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-96 h-96 bg-yellow-400/5 rounded-full blur-3xl pointer-events-none" />

    <div class="relative z-10 w-full max-w-sm px-4">
      <div class="flex flex-col items-center mb-8">
        <div class="flex items-center justify-center w-16 h-16 rounded-2xl bg-yellow-400/10 border border-yellow-400/20 mb-4 shadow-[0_0_24px_rgba(255,198,0,0.15)]">
          <UIcon name="i-lucide-zap" class="w-9 h-9 text-yellow-400 drop-shadow-[0_0_8px_rgba(255,198,0,0.7)]" />
        </div>
        <h1 class="text-3xl font-black tracking-widest uppercase text-yellow-400 drop-shadow-[0_0_12px_rgba(255,198,0,0.4)]">
          wireops
        </h1>
        <p class="text-sm text-wire-400 mt-1 tracking-wide">GitOps Orchestrator</p>
      </div>

      <div class="rounded-2xl border border-carbon-800 bg-carbon-900 p-6 shadow-2xl">
        <!-- Loading -->
        <div v-if="state === 'loading'" class="flex items-center justify-center py-6">
          <UIcon name="i-lucide-loader-circle" class="w-6 h-6 animate-spin text-yellow-400" />
        </div>

        <!-- Invalid token -->
        <div v-else-if="state === 'invalid'" class="text-center space-y-4">
          <div class="flex items-center justify-center w-12 h-12 rounded-full bg-red-400/10 mx-auto">
            <UIcon name="i-lucide-x-circle" class="w-6 h-6 text-red-400" />
          </div>
          <p class="text-sm text-gray-300">{{ stateError }}</p>
          <NuxtLink to="/login" class="text-xs text-yellow-400 hover:underline">Back to login</NuxtLink>
        </div>

        <!-- Success -->
        <div v-else-if="state === 'success'" class="text-center space-y-4">
          <div class="flex items-center justify-center w-12 h-12 rounded-full bg-green-400/10 mx-auto">
            <UIcon name="i-lucide-check-circle" class="w-6 h-6 text-green-400" />
          </div>
          <p class="text-sm text-gray-300">Account created! Redirecting to login...</p>
        </div>

        <!-- Setup form -->
        <form v-else class="flex flex-col gap-4" @submit.prevent="handleSubmit">
          <div class="mb-1">
            <h2 class="text-lg font-semibold">Set up your account</h2>
            <p class="text-xs text-gray-500 mt-1">Choose a password to complete your registration.</p>
          </div>

          <UAlert v-if="error" color="error" :title="error" icon="i-lucide-alert-circle" />

          <UFormField label="Email">
            <UInput :model-value="email" type="email" icon="i-lucide-mail" disabled class="w-full" />
          </UFormField>

          <UFormField label="Password">
            <UInput
              v-model="password"
              type="password"
              placeholder="••••••••"
              icon="i-lucide-lock"
              required
              class="w-full"
            />
          </UFormField>

          <UFormField label="Confirm Password">
            <UInput
              v-model="passwordConfirm"
              type="password"
              placeholder="••••••••"
              icon="i-lucide-lock"
              required
              class="w-full"
            />
          </UFormField>

          <UButton type="submit" block :loading="loading" icon="i-lucide-zap" label="Create Account" class="mt-1" />
        </form>
      </div>
    </div>
  </div>
</template>
