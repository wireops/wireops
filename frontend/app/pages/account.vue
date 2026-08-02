<script setup lang="ts">
import { ref } from 'vue'

const { user, logout } = useAuth()
const { updateSelf } = useApi()
const toast = useToast()

const profileForm = ref({ name: user.value?.name || '' })
const profileSaving = ref(false)

async function handleSaveProfile() {
  profileSaving.value = true
  try {
    const updated = await updateSelf({ name: profileForm.value.name.trim() })
    if (user.value) user.value.name = updated.name
    toast.add({ title: 'Profile updated', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to update profile', description: e?.message, color: 'error' })
  } finally {
    profileSaving.value = false
  }
}

const passwordForm = ref({ oldPassword: '', password: '', passwordConfirm: '' })
const passwordSaving = ref(false)

async function handleChangePassword() {
  if (
    !passwordForm.value.oldPassword.trim()
    || !passwordForm.value.password.trim()
    || !passwordForm.value.passwordConfirm.trim()
  ) {
    toast.add({ title: 'All password fields are required', color: 'error' })
    return
  }
  if (passwordForm.value.password !== passwordForm.value.passwordConfirm) {
    toast.add({ title: 'Passwords do not match', color: 'error' })
    return
  }
  passwordSaving.value = true
  try {
    await updateSelf({
      old_password: passwordForm.value.oldPassword,
      password: passwordForm.value.password,
      password_confirm: passwordForm.value.passwordConfirm,
    })
    passwordForm.value = { oldPassword: '', password: '', passwordConfirm: '' }
    toast.add({ title: 'Password updated', description: 'Please sign in again with your new password.', color: 'success' })
    logout()
  } catch (e: any) {
    toast.add({ title: 'Failed to update password', description: e?.message, color: 'error' })
  } finally {
    passwordSaving.value = false
  }
}
</script>

<template>
  <div class="max-w-2xl space-y-6">
    <h1 class="flex items-center gap-3 text-2xl font-bold text-gray-900 dark:text-wire-200">
      <span class="inline-flex items-center justify-center w-9 h-9 rounded-lg bg-yellow-400/10">
        <UIcon name="i-lucide-user-circle" class="w-5 h-5 text-yellow-400" />
      </span>
      <span>
        <span class="block">Account</span>
        <span class="block text-sm font-normal text-gray-500 dark:text-wire-200/60">Your profile and login credentials.</span>
      </span>
    </h1>

    <UCard>
      <template #header><h3 class="font-semibold">Profile</h3></template>
      <form class="space-y-4" @submit.prevent="handleSaveProfile">
        <UFormField label="Name">
          <AppTextInput v-model="profileForm.name" placeholder="Your name" icon="i-lucide-user" aria-label="Name" class="w-full" />
        </UFormField>
        <UFormField label="Email">
          <AppTextInput :model-value="user?.email" disabled icon="i-lucide-mail" aria-label="Email" class="w-full" />
        </UFormField>
        <UButton type="submit" label="Save Profile" icon="i-lucide-check" :loading="profileSaving" />
      </form>
    </UCard>

    <UCard>
      <template #header><h3 class="font-semibold">Change Password</h3></template>
      <form class="space-y-4" @submit.prevent="handleChangePassword">
        <UFormField label="Current Password">
          <AppTextInput v-model="passwordForm.oldPassword" type="password" placeholder="••••••••" icon="i-lucide-lock" aria-label="Current password" class="w-full" />
        </UFormField>
        <UFormField label="New Password">
          <AppTextInput v-model="passwordForm.password" type="password" placeholder="••••••••" icon="i-lucide-lock" aria-label="New password" class="w-full" />
        </UFormField>
        <UFormField label="Confirm New Password">
          <AppTextInput v-model="passwordForm.passwordConfirm" type="password" placeholder="••••••••" icon="i-lucide-lock" aria-label="Confirm new password" class="w-full" />
        </UFormField>
        <UButton type="submit" label="Update Password" icon="i-lucide-check" :loading="passwordSaving" />
      </form>
    </UCard>
  </div>
</template>
