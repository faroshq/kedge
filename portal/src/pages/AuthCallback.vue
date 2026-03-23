<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { parseClusterName } from '@/auth/token'
import type { LoginResponse, StoredAuth } from '@/auth/types'

const router = useRouter()
const auth = useAuthStore()
const error = ref<string | null>(null)

onMounted(() => {
  try {
    const params = new URLSearchParams(window.location.search)
    const encoded = params.get('response')
    if (!encoded) {
      error.value = 'Missing response parameter'
      return
    }

    // Decode base64url LoginResponse from hub callback
    const json = atob(encoded.replace(/-/g, '+').replace(/_/g, '/'))
    const resp: LoginResponse = JSON.parse(json)

    const kubeconfigStr = resp.kubeconfig
      ? atob(typeof resp.kubeconfig === 'string' ? resp.kubeconfig : '')
      : ''
    const clusterName = parseClusterName(kubeconfigStr)

    const stored: StoredAuth = {
      idToken: resp.idToken ?? '',
      refreshToken: resp.refreshToken,
      expiresAt: resp.expiresAt ?? 0,
      issuerUrl: resp.issuerUrl,
      clientId: resp.clientId,
      email: resp.email ?? '',
      userId: resp.userId ?? '',
      clusterName,
    }

    auth.loginFromOIDCResponse(stored)

    // Clean up session storage
    sessionStorage.removeItem('oidc_verifier')
    sessionStorage.removeItem('oidc_session')

    router.push('/')
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Failed to process auth callback'
  }
})
</script>

<template>
  <div class="flex min-h-screen items-center justify-center bg-gray-50">
    <div v-if="error" class="rounded-lg border border-red-200 bg-white p-8 text-center">
      <p class="text-sm text-red-600">{{ error }}</p>
      <router-link to="/login" class="mt-4 inline-block text-sm text-gray-600 hover:underline">
        Back to login
      </router-link>
    </div>
    <div v-else class="text-sm text-gray-500">Completing sign in...</div>
  </div>
</template>
