<script setup lang="ts">
import { useAuthStore } from '@/stores/auth'
import { useRouter } from 'vue-router'

const auth = useAuthStore()
const router = useRouter()

function handleLogout() {
  auth.logout()
  router.push('/login')
}
</script>

<template>
  <header class="flex h-14 items-center justify-between border-b border-gray-200 bg-white px-6">
    <div class="text-sm text-gray-500">
      <span v-if="auth.clusterName">Cluster: {{ auth.clusterName }}</span>
    </div>
    <div class="flex items-center gap-4">
      <span v-if="auth.user" class="text-sm text-gray-700">{{ auth.user.email }}</span>
      <button
        class="rounded-md px-3 py-1.5 text-sm font-medium text-gray-600 hover:bg-gray-100"
        @click="handleLogout"
      >
        Logout
      </button>
    </div>
  </header>
</template>
