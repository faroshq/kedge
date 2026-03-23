<script setup lang="ts">
import { Loader2, Inbox, AlertCircle } from 'lucide-vue-next'

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
  <div class="overflow-hidden rounded-xl border border-border-subtle bg-surface-raised">
    <!-- Error -->
    <div v-if="error" class="flex items-center gap-2 p-4 text-[13px] text-danger">
      <AlertCircle class="h-4 w-4 shrink-0" :stroke-width="1.75" />
      {{ error }}
    </div>

    <!-- Loading skeleton -->
    <div v-else-if="loading" class="flex flex-col items-center justify-center py-12">
      <Loader2 class="h-5 w-5 animate-spin text-text-muted" :stroke-width="1.75" />
      <p class="mt-2 text-[13px] text-text-muted">Loading...</p>
    </div>

    <!-- Table -->
    <table v-else class="min-w-full">
      <thead>
        <tr class="border-b border-border-subtle">
          <th
            v-for="col in columns"
            :key="col.key"
            class="px-4 py-3 text-left text-[11px] font-semibold uppercase tracking-wider text-text-muted"
          >
            {{ col.label }}
          </th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="(row, i) in rows"
          :key="i"
          class="group cursor-pointer border-b border-border-subtle transition-colors duration-100 last:border-b-0 hover:bg-surface-hover"
          @click="$emit('rowClick', row)"
        >
          <td
            v-for="col in columns"
            :key="col.key"
            class="whitespace-nowrap px-4 py-3 text-[13px] text-text-secondary transition-colors duration-100 group-hover:text-text-primary"
          >
            <slot :name="col.key" :value="row[col.key]" :row="row">
              {{ row[col.key] }}
            </slot>
          </td>
        </tr>
        <tr v-if="rows.length === 0">
          <td :colspan="columns.length" class="py-12 text-center">
            <Inbox class="mx-auto h-8 w-8 text-text-muted/50" :stroke-width="1.25" />
            <p class="mt-2 text-[13px] text-text-muted">No data</p>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
