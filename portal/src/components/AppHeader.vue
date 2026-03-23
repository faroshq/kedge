<script setup lang="ts">
import { useAuthStore } from '@/stores/auth'
import { useRouter } from 'vue-router'
import { LogOut, Terminal, CircleUser } from 'lucide-vue-next'

const auth = useAuthStore()
const router = useRouter()

function handleLogout() {
  auth.logout()
  router.push('/login')
}
</script>

<template>
  <header class="flex h-12 items-center justify-between border-b border-border-subtle bg-surface/80 px-6 backdrop-blur-xl">
    <!-- Cluster badge -->
    <div v-if="auth.clusterName" class="flex items-center gap-2">
      <Terminal class="h-3.5 w-3.5 text-accent/60" :stroke-width="1.75" />
      <span class="font-mono text-[11px] tracking-wider text-text-muted">{{ auth.clusterName }}</span>
    </div>
    <div v-else />

    <!-- User -->
    <div class="flex items-center gap-2">
      <div v-if="auth.user" class="flex items-center gap-2 rounded-full border border-border-subtle bg-surface-overlay/50 px-3 py-1">
        <CircleUser class="h-3.5 w-3.5 text-text-muted" :stroke-width="1.5" />
        <span class="text-[11px] text-text-secondary">{{ auth.user.email }}</span>
      </div>
      <button
        class="flex h-7 w-7 items-center justify-center rounded-full border border-border-subtle text-text-muted transition-all duration-200 hover:border-danger/30 hover:bg-danger-subtle hover:text-danger"
        title="Logout"
        @click="handleLogout"
      >
        <LogOut class="h-3 w-3" :stroke-width="2" />
      </button>
    </div>
  </header>
</template>
