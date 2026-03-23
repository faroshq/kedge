<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { parseClusterName } from '@/auth/token'
import type { LoginResponse, StoredAuth } from '@/auth/types'
import { AlertCircle, ArrowLeft, Hexagon } from 'lucide-vue-next'

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
  <div class="dot-grid relative flex min-h-screen items-center justify-center bg-surface">
    <div class="pointer-events-none fixed inset-0 overflow-hidden">
      <div class="absolute -top-40 left-1/2 h-80 w-[500px] -translate-x-1/2 rounded-full bg-accent/6 blur-[120px]" />
    </div>

    <div v-if="error" class="card-glow relative rounded-2xl border border-border-subtle bg-surface-raised/90 p-8 text-center shadow-2xl shadow-black/30 backdrop-blur-xl">
      <div class="mx-auto flex h-10 w-10 items-center justify-center rounded-full border border-danger/20 bg-danger-subtle">
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

    <div v-else class="relative flex flex-col items-center gap-4">
      <div class="relative flex h-14 w-14 items-center justify-center">
        <div class="absolute inset-0 animate-pulse rounded-xl bg-accent/20 blur-md" />
        <div class="relative flex h-14 w-14 items-center justify-center rounded-xl border border-accent/20 bg-surface-overlay">
          <Hexagon class="h-7 w-7 animate-spin text-accent" style="animation-duration: 3s" :stroke-width="2" />
        </div>
      </div>
      <p class="text-[13px] text-text-muted">Completing sign in...</p>
    </div>
  </div>
</template>
