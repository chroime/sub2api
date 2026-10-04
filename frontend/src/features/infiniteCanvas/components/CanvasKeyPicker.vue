<script setup lang="ts">
import type { CanvasKeyOption } from '../keySelection'

defineProps<{ options: CanvasKeyOption[]; modelValue: number | null; loading?: boolean }>()
const emit = defineEmits<{ (event: 'update:modelValue', value: number | null): void; (event: 'create-key'): void }>()
</script>

<template>
  <div class="canvas-key-picker space-y-2">
    <label class="block text-xs font-medium text-gray-600 dark:text-gray-300" for="canvas-key">Image API key</label>
    <select id="canvas-key" :value="modelValue ?? ''" class="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-dark-600 dark:bg-dark-800" :disabled="loading || !options.length" @change="emit('update:modelValue', ($event.target as HTMLSelectElement).value ? Number(($event.target as HTMLSelectElement).value) : null)">
      <option value="">{{ loading ? 'Loading keys…' : options.length ? 'Select a key' : 'No eligible image keys' }}</option>
      <option v-for="option in options" :key="option.id" :value="option.id" data-canvas-key-option>{{ option.name }} · {{ option.groupName }} · {{ option.maskedKey }}</option>
    </select>
    <p v-if="!loading && !options.length" data-canvas-empty="keys" class="text-xs text-gray-500 dark:text-dark-400">No eligible image key. <a href="/keys" data-create-key-link class="text-primary-600 hover:underline">Create a key</a> to get started.</p>
    <button type="button" class="text-xs text-primary-600 hover:underline" @click="emit('create-key')">Create key</button>
  </div>
</template>
