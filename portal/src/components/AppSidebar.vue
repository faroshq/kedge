<script setup lang="ts">
import { useRoute } from 'vue-router'
import { LayoutDashboard, Server, Hexagon } from 'lucide-vue-next'
import { computed } from 'vue'

const route = useRoute()

const navItems = [
  { label: 'Dashboard', to: '/', icon: LayoutDashboard },
  { label: 'Edges', to: '/edges', icon: Server },
]

function isActive(path: string) {
  if (path === '/') return route.path === '/'
  return route.path.startsWith(path)
}

const currentYear = computed(() => new Date().getFullYear())
</script>

<template>
  <aside class="flex h-full w-60 flex-col bg-surface-raised border-r border-border-subtle">
    <!-- Logo -->
    <div class="flex h-16 items-center gap-2.5 px-5">
      <div class="flex h-8 w-8 items-center justify-center rounded-lg bg-accent/15">
        <Hexagon class="h-4 w-4 text-accent" :stroke-width="2.5" />
      </div>
      <span class="text-[15px] font-semibold tracking-tight text-text-primary">Kedge</span>
    </div>

    <!-- Nav -->
    <nav class="flex-1 space-y-0.5 px-3 pt-2">
      <router-link
        v-for="item in navItems"
        :key="item.to"
        :to="item.to"
        class="group flex items-center gap-3 rounded-lg px-3 py-2 text-[13px] font-medium transition-all duration-150"
        :class="
          isActive(item.to)
            ? 'bg-accent/10 text-accent-hover'
            : 'text-text-secondary hover:bg-surface-hover hover:text-text-primary'
        "
      >
        <component
          :is="item.icon"
          class="h-4 w-4 shrink-0 transition-colors duration-150"
          :class="isActive(item.to) ? 'text-accent' : 'text-text-muted group-hover:text-text-secondary'"
          :stroke-width="isActive(item.to) ? 2.25 : 1.75"
        />
        {{ item.label }}
        <div
          v-if="isActive(item.to)"
          class="ml-auto h-1.5 w-1.5 rounded-full bg-accent"
        />
      </router-link>
    </nav>

    <!-- Footer -->
    <div class="border-t border-border-subtle px-5 py-3">
      <p class="text-[11px] text-text-muted">&copy; {{ currentYear }} Kedge</p>
    </div>
  </aside>
</template>
