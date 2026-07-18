<script setup lang="ts">
import { ref, watch } from 'vue'

const props = defineProps<{
  integration: any
}>()

const emit = defineEmits<{
  saved: []
}>()

const isOpen = defineModel<boolean>('open', { default: false })
const loading = ref(false)
const hasSecret = ref(false)

const form = ref({
  bucket: '',
  region: '',
  endpoint: '',
  prefix: '',
  accessKey: '',
  secret: '',
  forcePathStyle: true,
  encryptContent: true,
  kmsEnabled: false,
  kmsKeyId: '',
  kmsRegion: '',
})

watch(() => props.integration, (newVal) => {
  if (newVal) {
    const config = newVal.config || {}
    form.value.bucket = config.bucket || ''
    form.value.region = config.region || ''
    form.value.endpoint = config.endpoint || ''
    form.value.prefix = config.prefix || ''
    form.value.accessKey = config.access_key || ''
    form.value.secret = config.secret || ''
    form.value.forcePathStyle = config.force_path_style ?? true
    form.value.encryptContent = config.encrypt_content ?? true
    form.value.kmsEnabled = config.kms_enabled || false
    form.value.kmsKeyId = config.kms_key_id || ''
    form.value.kmsRegion = config.kms_region || ''
    hasSecret.value = config.secret === '••••••••'
  }
}, { immediate: true, deep: true })

const toast = useToast()
const { saveIntegration } = useIntegrations()

function close() {
  isOpen.value = false
}

function onSecretFocus() {
  if (hasSecret.value && form.value.secret === '••••••••') {
    form.value.secret = ''
    hasSecret.value = false
  }
}

async function handleSave() {
  if (!form.value.bucket || !form.value.region || !form.value.accessKey || !form.value.secret) {
    toast.add({ title: 'Bucket, region, access key and secret key are required', color: 'error' })
    return
  }
  if (form.value.bucket.includes('/')) {
    toast.add({ title: 'Bucket must not include a path/prefix', description: 'Use the Prefix field instead — most S3-compatible providers treat the bucket name as one literal path segment.', color: 'error' })
    return
  }
  loading.value = true
  try {
    const success = await saveIntegration('s3', props.integration.enabled, {
      bucket: form.value.bucket,
      region: form.value.region,
      endpoint: form.value.endpoint,
      prefix: form.value.prefix,
      access_key: form.value.accessKey,
      secret: form.value.secret,
      force_path_style: form.value.forcePathStyle,
      encrypt_content: form.value.encryptContent,
      kms_enabled: form.value.kmsEnabled,
      kms_key_id: form.value.kmsKeyId,
      kms_region: form.value.kmsRegion,
    })
    if (success) {
      toast.add({ title: 'S3 storage settings saved', color: 'success' })
      emit('saved')
      close()
    } else {
      toast.add({ title: 'Failed to save settings', color: 'error' })
    }
  } catch (e: any) {
    toast.add({ title: 'Error saving settings', description: e.message, color: 'error' })
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <UModal
    v-model:open="isOpen"
    title="Configure S3 Storage"
    description="Mirror backups to S3-compatible storage (AWS S3, R2, MinIO, B2, ...)."
  >
    <template #body>
      <div class="space-y-4" role="document">
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <UFormField label="Bucket" required>
            <AppTextInput v-model="form.bucket" placeholder="my-wireops-backups" class="font-mono text-sm" />
          </UFormField>
          <UFormField label="Region" required>
            <AppTextInput v-model="form.region" placeholder="us-east-1" class="font-mono text-sm" />
          </UFormField>
        </div>

        <UFormField label="Endpoint">
          <AppTextInput v-model="form.endpoint" placeholder="https://s3.us-east-1.amazonaws.com" class="font-mono text-sm" />
        </UFormField>

        <UFormField label="Prefix (optional)">
          <AppTextInput v-model="form.prefix" placeholder="wireops" class="font-mono text-sm" />
          <p class="text-xs text-gray-400 mt-1">Keeps backups under a sub-path within the bucket. Created automatically on save if it doesn't exist yet.</p>
        </UFormField>

        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <UFormField label="Access Key" required>
            <AppTextInput v-model="form.accessKey" class="font-mono text-sm" />
          </UFormField>
          <UFormField label="Secret Key" required>
            <AppTextInput
              v-model="form.secret"
              :type="hasSecret && form.secret === '••••••••' ? 'password' : 'text'"
              class="font-mono text-sm"
              @focus="onSecretFocus"
            />
            <p class="text-xs text-gray-400 mt-1">Stored encrypted at rest under SECRET_KEY.</p>
          </UFormField>
        </div>

        <div class="flex items-center gap-2">
          <USwitch v-model="form.forcePathStyle" />
          <span class="text-sm">Force path-style addressing</span>
        </div>
        <p class="text-xs text-gray-400 -mt-2">Required for most self-hosted S3-compatible services (e.g. MinIO); leave off for AWS S3.</p>

        <div class="flex items-center gap-2">
          <USwitch v-model="form.encryptContent" />
          <span class="text-sm">Encrypt backup content before upload</span>
        </div>
        <p class="text-xs text-gray-400 -mt-2">On by default. Uses SECRET_KEY unless KMS is enabled below.</p>

        <div class="flex items-center gap-2">
          <USwitch v-model="form.kmsEnabled" />
          <span class="text-sm">Use AWS KMS for content encryption</span>
        </div>

        <div v-if="form.kmsEnabled" class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <UFormField label="KMS Key ID">
            <AppTextInput v-model="form.kmsKeyId" placeholder="alias/wireops-backups" class="font-mono text-sm" />
          </UFormField>
          <UFormField label="KMS Region (optional)">
            <AppTextInput v-model="form.kmsRegion" placeholder="defaults to Region above" class="font-mono text-sm" />
          </UFormField>
        </div>
      </div>
    </template>

    <template #footer>
      <div class="flex w-full items-center gap-2">
        <UButton label="Cancel" variant="outline" @click="close" />
        <UButton
          label="Save Settings"
          color="primary"
          class="ml-auto"
          :loading="loading"
          @click="handleSave"
        />
      </div>
    </template>
  </UModal>
</template>
