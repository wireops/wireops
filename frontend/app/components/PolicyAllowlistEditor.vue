<script setup lang="ts">
import { ref, watch, nextTick } from 'vue'

const list = defineModel<string[]>({ required: true })

const props = defineProps<{
  placeholder: string
  emptyText: string
  addLabel: string
}>()

const emit = defineEmits<{
  (e: 'save'): void
}>()

const activeEditIndex = ref<number | null>(null)
const originalValues = ref<Record<number, string | undefined>>({})
const containerEl = ref<HTMLElement | null>(null)

// Stable per-row identity for v-for keys, independent of value/index so
// input focus/lock state survives inserts and deletes elsewhere in the list.
let idCounter = 0
function genId() {
  return idCounter++
}
const keys = ref<number[]>(list.value.map(() => genId()))

// Parent reassigns the whole array on load/save (new reference) — resync keys then.
// Internal push/splice below mutate the same reference and keep keys in lockstep.
watch(
  () => list.value,
  (newList, oldList) => {
    if (newList !== oldList) {
      keys.value = newList.map(() => genId())
    }
  },
)

function pushEntry(value: string) {
  list.value.push(value)
  keys.value.push(genId())
}

function spliceEntry(index: number) {
  list.value.splice(index, 1)
  keys.value.splice(index, 1)
}

function focusInputAt(index: number) {
  nextTick(() => {
    const inputs = containerEl.value?.querySelectorAll('input')
    const target = inputs?.[index] as HTMLInputElement | null
    target?.focus()
  })
}

function unlock(index: number) {
  activeEditIndex.value = index
  focusInputAt(index)
}

function lock(index: number, event?: Event) {
  activeEditIndex.value = null
  if (event) {
    const target = event.currentTarget as HTMLElement
    const parent = target.closest('.flex')
    const input = parent?.querySelector('input') as HTMLInputElement | null
    input?.blur()
  }
}

function addEntry() {
  const len = list.value.length
  if (len > 0 && !list.value[len - 1].trim()) {
    activeEditIndex.value = len - 1
    focusInputAt(len - 1)
    return
  }
  pushEntry('')
  activeEditIndex.value = list.value.length - 1
  focusInputAt(list.value.length - 1)
}

function onFocusInput(index: number) {
  originalValues.value[index] = list.value[index]
  activeEditIndex.value = index
}

function onBlurInput(val: string, index: number) {
  activeEditIndex.value = null
  const origVal = originalValues.value[index] ?? ''

  setTimeout(() => {
    if (index < list.value.length && typeof list.value[index] === 'string' && !list.value[index].trim()) {
      if (origVal.trim() === '') {
        spliceEntry(index)
      } else {
        list.value[index] = origVal
      }
    } else if (val !== origVal && index < list.value.length && typeof list.value[index] === 'string') {
      emit('save')
    }
    originalValues.value[index] = undefined
  }, 150)
}

const showDeleteRuleModal = ref(false)
const deleteRuleIndex = ref<number | null>(null)
const deleteRuleValue = ref('')

function requestDelete(index: number, value: string) {
  if (!value.trim()) {
    executeDelete(index)
    return
  }
  deleteRuleIndex.value = index
  deleteRuleValue.value = value
  showDeleteRuleModal.value = true
}

function executeDelete(index: number) {
  spliceEntry(index)
  emit('save')
  showDeleteRuleModal.value = false
  deleteRuleIndex.value = null
}
</script>

<template>
  <div class="space-y-2">
    <div ref="containerEl" class="space-y-2">
      <div
        v-for="(_, i) in list"
        :key="keys[i]"
        class="flex items-center gap-2"
      >
        <UButton
          :icon="activeEditIndex === i ? 'i-lucide-lock-open' : 'i-lucide-lock'"
          variant="ghost"
          :color="activeEditIndex === i ? 'primary' : 'neutral'"
          size="xs"
          class="shrink-0"
          :aria-label="activeEditIndex === i ? 'Lock' : 'Unlock'"
          tabindex="-1"
          @click.stop="activeEditIndex === i ? lock(i, $event) : unlock(i)"
        />
        <AppTextInput
          v-model="list[i]"
          :placeholder="props.placeholder"
          class="flex-1 font-mono text-sm"
          :readonly="activeEditIndex !== i"
          @click="unlock(i)"
          @focus="onFocusInput(i)"
          @blur="onBlurInput(list[i], i)"
          @keyup.enter="($event.target as any).blur()"
        />
        <UButton
          icon="i-lucide-x"
          variant="ghost"
          color="error"
          size="xs"
          class="shrink-0"
          aria-label="Delete entry"
          @click.stop="requestDelete(i, list[i])"
          @keydown.enter.prevent.stop="requestDelete(i, list[i])"
          @keyup.enter.prevent.stop
        />
      </div>
      <div v-if="list.length === 0" class="text-xs text-gray-400 italic ml-6">{{ props.emptyText }}</div>
    </div>

    <div class="ml-6">
      <UButton icon="i-lucide-plus" size="xs" variant="outline" :label="props.addLabel" @click="addEntry" />
    </div>

    <UModal v-if="showDeleteRuleModal" v-model:open="showDeleteRuleModal">
      <template #content>
        <DeleteRuleModal
          :value="deleteRuleValue"
          @confirm="executeDelete(deleteRuleIndex!)"
          @cancel="showDeleteRuleModal = false"
        />
      </template>
    </UModal>
  </div>
</template>
