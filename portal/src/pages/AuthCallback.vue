<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { parseClusterName } from '@/auth/token'
import type { LoginResponse, StoredAuth } from '@/auth/types'
import { Loader2, AlertCircle, ArrowLeft } from 'lucide-vue-next'

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
    sessionStorage.removeItem('oidc_verifier')
    sessionStorage.removeItem('oidc_session')
    router.push('/')
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Failed to process auth callback'
  }
})
</script>

<template>
  <div class="flex min-h-screen items-center justify-center bg-surface">
    <div v-if="error" class="rounded-2xl border border-border-subtle bg-surface-raised p-8 text-center shadow-2xl shadow-black/20">
      <div class="mx-auto flex h-10 w-10 items-center justify-center rounded-full bg-danger-subtle">
        <AlertCircle class="h-5 w-5 text-danger" :stroke-width="1.75" />
      </div>
      <p class="mt-3 text-[13px] text-text-secondary">{{ error }}</p>
      <router-link
        to="/login"
        class="mt-4 inline-flex items-center gap-1.5 text-[13px] font-medium text-accent transition-colors hover:text-accent-hover"
      >
        <ArrowLeft class="h-3.5 w-3.5" :stroke-width="1.75" />
        Back to login
      </router-link>
    </div>
    <div v-else class="flex flex-col items-center gap-3">
      <Loader2 class="h-6 w-6 animate-spin text-accent" :stroke-width="1.75" />
      <p class="text-[13px] text-text-muted">Completing sign in...</p>
    </div>
  </div>
</template>
