<script setup lang="ts">
defineProps<{
  columns: Array<{ key: string; label: string }>
  rows: Array<Record<string, unknown>>
  loading?: boolean
  error?: string | null
}>()

defineEmits<{
  rowClick: [row: Record<string, unknown>]
}>()
</script>

<template>
  <div class="overflow-hidden rounded-lg border border-gray-200 bg-white">
    <div v-if="error" class="p-4 text-sm text-red-600">{{ error }}</div>
    <div v-else-if="loading" class="p-4 text-sm text-gray-500">Loading...</div>
    <table v-else class="min-w-full divide-y divide-gray-200">
      <thead class="bg-gray-50">
        <tr>
          <th
            v-for="col in columns"
            :key="col.key"
            class="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500"
          >
            {{ col.label }}
          </th>
        </tr>
      </thead>
      <tbody class="divide-y divide-gray-200">
        <tr
          v-for="(row, i) in rows"
          :key="i"
          class="cursor-pointer hover:bg-gray-50"
          @click="$emit('rowClick', row)"
        >
          <td
            v-for="col in columns"
            :key="col.key"
            class="whitespace-nowrap px-4 py-3 text-sm text-gray-900"
          >
            <slot :name="col.key" :value="row[col.key]" :row="row">
              {{ row[col.key] }}
            </slot>
          </td>
        </tr>
        <tr v-if="rows.length === 0">
          <td :colspan="columns.length" class="px-4 py-6 text-center text-sm text-gray-500">
            No data
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
