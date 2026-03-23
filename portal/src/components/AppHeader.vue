<script setup lang="ts">
import { useAuthStore } from '@/stores/auth'
import { useRouter } from 'vue-router'
import { LogOut, User, Database } from 'lucide-vue-next'

const auth = useAuthStore()
const router = useRouter()

function handleLogout() {
  auth.logout()
  router.push('/login')
}
</script>

<template>
  <header class="flex h-14 items-center justify-between border-b border-border-subtle bg-surface-raised/50 px-6 backdrop-blur-sm">
    <div class="flex items-center gap-2 text-[13px] text-text-muted">
      <Database class="h-3.5 w-3.5" :stroke-width="1.75" />
      <span v-if="auth.clusterName" class="font-mono text-text-secondary">{{ auth.clusterName }}</span>
    </div>
    <div class="flex items-center gap-3">
      <div v-if="auth.user" class="flex items-center gap-2 rounded-lg bg-surface-overlay px-3 py-1.5">
        <User class="h-3.5 w-3.5 text-text-muted" :stroke-width="1.75" />
        <span class="text-[13px] text-text-secondary">{{ auth.user.email }}</span>
      </div>
      <button
        class="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-[13px] font-medium text-text-muted transition-all duration-150 hover:bg-danger-subtle hover:text-danger"
        @click="handleLogout"
      >
        <LogOut class="h-3.5 w-3.5" :stroke-width="1.75" />
        Logout
      </button>
    </div>
  </header>
</template>
