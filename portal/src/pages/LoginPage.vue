<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { API_PATHS } from '@/lib/constants'
import { Hexagon, KeyRound, ShieldCheck, Loader2, AlertCircle } from 'lucide-vue-next'

const auth = useAuthStore()
const router = useRouter()
const tokenInput = ref('')
const loginError = ref<string | null>(null)
const inputFocused = ref(false)

onMounted(async () => {
  if (auth.isAuthenticated) {
    router.push('/')
    return
  }
  await auth.detectAuthMode()
})

async function handleTokenLogin() {
  loginError.value = null
  try {
    await auth.loginStatic(tokenInput.value)
    router.push('/')
  } catch (e) {
    loginError.value = e instanceof Error ? e.message : 'Login failed'
  }
}

function handleOIDCLogin() {
  const array = new Uint8Array(32)
  crypto.getRandomValues(array)
  const codeVerifier = btoa(String.fromCharCode(...array))
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '')

  const sessionId = crypto.randomUUID()

  sessionStorage.setItem('oidc_verifier', codeVerifier)
  sessionStorage.setItem('oidc_session', sessionId)

  const callbackUrl = `${window.location.origin}/auth/callback`
  const params = new URLSearchParams({
    redirect_uri: callbackUrl,
    s: sessionId,
    v: codeVerifier,
  })

  window.location.href = `${API_PATHS.authorize}?${params.toString()}`
}
</script>

<template>
  <div class="flex min-h-screen items-center justify-center bg-surface">
    <!-- Subtle background gradient -->
    <div class="pointer-events-none fixed inset-0 overflow-hidden">
      <div class="absolute -top-40 left-1/2 h-80 w-[600px] -translate-x-1/2 rounded-full bg-accent/5 blur-3xl" />
    </div>

    <div class="relative w-full max-w-sm space-y-6 rounded-2xl border border-border-subtle bg-surface-raised p-8 shadow-2xl shadow-black/20">
      <!-- Logo -->
      <div class="text-center">
        <div class="mx-auto flex h-12 w-12 items-center justify-center rounded-xl bg-accent/15">
          <Hexagon class="h-6 w-6 text-accent" :stroke-width="2" />
        </div>
        <h1 class="mt-4 text-xl font-semibold tracking-tight text-text-primary">Welcome to Kedge</h1>
        <p class="mt-1 text-[13px] text-text-muted">Sign in to manage your edges</p>
      </div>

      <!-- Error -->
      <transition name="fade">
        <div v-if="loginError" class="flex items-center gap-2 rounded-lg bg-danger-subtle p-3 text-[13px] text-danger">
          <AlertCircle class="h-4 w-4 shrink-0" :stroke-width="1.75" />
          {{ loginError }}
        </div>
      </transition>

      <!-- OIDC Login -->
      <button
        v-if="auth.authMode === 'both' || auth.authMode === 'oidc'"
        class="group flex w-full items-center justify-center gap-2 rounded-xl bg-accent px-4 py-2.5 text-[13px] font-semibold text-white transition-all duration-200 hover:bg-accent-hover hover:shadow-lg hover:shadow-accent/20 active:scale-[0.98]"
        @click="handleOIDCLogin"
      >
        <ShieldCheck class="h-4 w-4 transition-transform duration-200 group-hover:scale-110" :stroke-width="2" />
        Sign in with SSO
      </button>

      <!-- Divider -->
      <div
        v-if="auth.authMode === 'both'"
        class="relative flex items-center py-1"
      >
        <div class="flex-grow border-t border-border-subtle"></div>
        <span class="mx-3 text-[11px] font-medium uppercase tracking-wider text-text-muted">or</span>
        <div class="flex-grow border-t border-border-subtle"></div>
      </div>

      <!-- Static Token Login -->
      <form @submit.prevent="handleTokenLogin" class="space-y-4">
        <div>
          <label for="token" class="mb-1.5 block text-[13px] font-medium text-text-secondary">Bearer Token</label>
          <div
            class="flex items-center gap-2 rounded-xl border bg-surface-overlay px-3 py-2.5 transition-all duration-200"
            :class="inputFocused ? 'border-accent/50 shadow-[0_0_0_3px_rgba(99,102,241,0.1)]' : 'border-border-default'"
          >
            <KeyRound class="h-4 w-4 shrink-0 text-text-muted" :stroke-width="1.75" />
            <input
              id="token"
              v-model="tokenInput"
              type="password"
              placeholder="Enter your token"
              class="w-full bg-transparent text-[13px] text-text-primary placeholder-text-muted outline-none"
              @focus="inputFocused = true"
              @blur="inputFocused = false"
            />
          </div>
        </div>
        <button
          type="submit"
          :disabled="!tokenInput || auth.loading"
          class="group flex w-full items-center justify-center gap-2 rounded-xl border border-border-default bg-surface-overlay px-4 py-2.5 text-[13px] font-semibold text-text-primary transition-all duration-200 hover:border-border-subtle hover:bg-surface-hover active:scale-[0.98] disabled:pointer-events-none disabled:opacity-40"
        >
          <Loader2
            v-if="auth.loading"
            class="h-4 w-4 animate-spin"
            :stroke-width="2"
          />
          <KeyRound
            v-else
            class="h-4 w-4 text-text-muted transition-colors duration-200 group-hover:text-text-secondary"
            :stroke-width="1.75"
          />
          {{ auth.loading ? 'Signing in...' : 'Sign in with Token' }}
        </button>
      </form>
    </div>
  </div>
</template>
