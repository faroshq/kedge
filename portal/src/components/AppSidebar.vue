<script setup lang="ts">
import { useRoute } from 'vue-router'
import { LayoutDashboard, Server, Hexagon, Zap } from 'lucide-vue-next'
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
  <aside class="relative flex h-full w-56 flex-col border-r border-border-subtle bg-surface-raised/80 backdrop-blur-xl">
    <!-- Accent gradient bleed at top -->
    <div class="pointer-events-none absolute -top-20 left-1/2 h-40 w-40 -translate-x-1/2 rounded-full bg-accent/8 blur-3xl" />

    <!-- Logo -->
    <div class="relative flex h-16 items-center gap-3 px-5">
      <div class="relative flex h-8 w-8 items-center justify-center">
        <div class="absolute inset-0 rounded-lg bg-accent/20 blur-sm" />
        <div class="relative flex h-8 w-8 items-center justify-center rounded-lg border border-accent/20 bg-surface-overlay">
          <Hexagon class="h-4 w-4 text-accent" :stroke-width="2.5" />
        </div>
      </div>
      <div>
        <span class="text-[14px] font-bold tracking-tight text-text-primary">KEDGE</span>
        <div class="flex items-center gap-1">
          <Zap class="h-2.5 w-2.5 text-success" :stroke-width="2.5" fill="currentColor" />
          <span class="text-[10px] font-medium uppercase tracking-widest text-success">Online</span>
        </div>
      </div>
    </div>

    <!-- Nav -->
    <nav class="flex-1 space-y-1 px-3 pt-4">
      <p class="mb-2 px-3 text-[10px] font-semibold uppercase tracking-[0.15em] text-text-muted">Navigation</p>
      <router-link
        v-for="item in navItems"
        :key="item.to"
        :to="item.to"
        class="group relative flex items-center gap-3 rounded-lg px-3 py-2.5 text-[13px] font-medium transition-all duration-200"
        :class="
          isActive(item.to)
            ? 'text-text-primary'
            : 'text-text-secondary hover:text-text-primary'
        "
      >
        <!-- Active bg glow -->
        <div
          v-if="isActive(item.to)"
          class="absolute inset-0 rounded-lg border border-accent/15 bg-accent/8"
        />
        <!-- Active left accent bar -->
        <div
          v-if="isActive(item.to)"
          class="absolute left-0 top-1/2 h-4 w-[3px] -translate-y-1/2 rounded-r-full bg-accent shadow-[0_0_8px_rgba(124,91,245,0.5)]"
        />
        <component
          :is="item.icon"
          class="relative z-10 h-4 w-4 shrink-0 transition-all duration-200"
          :class="isActive(item.to) ? 'text-accent' : 'text-text-muted group-hover:text-text-secondary'"
          :stroke-width="1.75"
        />
        <span class="relative z-10">{{ item.label }}</span>
      </router-link>
    </nav>

    <!-- Footer -->
    <div class="border-t border-border-subtle px-5 py-3">
      <p class="text-[10px] tracking-wide text-text-muted/60">&copy; {{ currentYear }} Kedge</p>
    </div>
  </aside>
</template>
