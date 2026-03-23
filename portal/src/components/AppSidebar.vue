<script setup lang="ts">
import { useRoute } from 'vue-router'

const route = useRoute()

const navItems = [
  { label: 'Dashboard', to: '/', icon: '⊞' },
  { label: 'Edges', to: '/edges', icon: '⬡' },
]

function isActive(path: string) {
  if (path === '/') return route.path === '/'
  return route.path.startsWith(path)
}
</script>

<template>
  <aside class="flex h-full w-56 flex-col border-r border-gray-200 bg-white">
    <div class="flex h-14 items-center border-b border-gray-200 px-4">
      <span class="text-lg font-semibold text-gray-900">Kedge</span>
    </div>
    <nav class="flex-1 space-y-1 p-3">
      <router-link
        v-for="item in navItems"
        :key="item.to"
        :to="item.to"
        class="flex items-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition-colors"
        :class="
          isActive(item.to)
            ? 'bg-gray-100 text-gray-900'
            : 'text-gray-600 hover:bg-gray-50 hover:text-gray-900'
        "
      >
        <span class="text-base">{{ item.icon }}</span>
        {{ item.label }}
      </router-link>
    </nav>
  </aside>
</template>
