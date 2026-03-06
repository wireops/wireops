<script setup lang="ts">
definePageMeta({ layout: false })

const { $pb } = useNuxtApp()

const email = ref('')
const loading = ref(false)
const sent = ref(false)
const error = ref('')

async function handleSubmit() {
  loading.value = true
  error.value = ''
  try {
    await $pb.collection('_superusers').requestPasswordReset(email.value)
    sent.value = true
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
        <div v-if="sent" class="text-center space-y-4">
          <div class="flex items-center justify-center w-12 h-12 rounded-full bg-green-400/10 mx-auto">
            <UIcon name="i-lucide-mail-check" class="w-6 h-6 text-green-400" />
          </div>
          <p class="text-sm text-gray-300">
            If an account with that email exists, a password reset link has been sent. Check your inbox.
          </p>
          <NuxtLink to="/login" class="text-xs text-yellow-400 hover:underline">
            Back to login
          </NuxtLink>
        </div>

        <form v-else class="flex flex-col gap-4" @submit.prevent="handleSubmit">
          <div class="mb-1">
            <h2 class="text-lg font-semibold">Forgot your password?</h2>
            <p class="text-xs text-gray-500 mt-1">Enter your email and we'll send you a reset link.</p>
          </div>

          <UAlert v-if="error" color="error" :title="error" icon="i-lucide-alert-circle" />

          <UFormField label="Email">
            <UInput
              v-model="email"
              type="email"
              placeholder="admin@example.com"
              icon="i-lucide-mail"
              required
              class="w-full"
            />
          </UFormField>

          <UButton type="submit" block :loading="loading" icon="i-lucide-send" label="Send Reset Link" class="mt-1" />

          <div class="text-center">
            <NuxtLink to="/login" class="text-xs text-gray-500 hover:text-yellow-400 transition-colors">
              Back to login
            </NuxtLink>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>
