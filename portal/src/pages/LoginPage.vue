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
  <div class="dot-grid relative flex min-h-screen items-center justify-center bg-surface">
    <!-- Ambient glows -->
    <div class="pointer-events-none fixed inset-0 overflow-hidden">
      <div class="absolute -top-40 left-1/2 h-80 w-[500px] -translate-x-1/2 rounded-full bg-accent/6 blur-[120px]" />
      <div class="absolute -bottom-20 right-1/4 h-60 w-60 rounded-full bg-success/4 blur-[100px]" />
    </div>

    <div class="relative w-full max-w-sm">
      <!-- Card -->
      <div class="card-glow space-y-6 rounded-2xl border border-border-subtle bg-surface-raised/90 p-8 shadow-2xl shadow-black/30 backdrop-blur-xl">
        <!-- Logo -->
        <div class="text-center">
          <div class="relative mx-auto flex h-14 w-14 items-center justify-center">
            <div class="absolute inset-0 rounded-xl bg-accent/20 blur-md" />
            <div class="relative flex h-14 w-14 items-center justify-center rounded-xl border border-accent/20 bg-surface-overlay">
              <Hexagon class="h-7 w-7 text-accent" :stroke-width="2" />
            </div>
          </div>
          <h1 class="text-gradient mt-5 text-xl font-bold tracking-tight">Welcome to Kedge</h1>
          <p class="mt-1.5 text-[13px] text-text-muted">Sign in to your command center</p>
        </div>

        <!-- Error -->
        <div v-if="loginError" class="flex items-center gap-2 rounded-lg border border-danger/20 bg-danger-subtle p-3 text-[13px] text-danger">
          <AlertCircle class="h-4 w-4 shrink-0" :stroke-width="1.75" />
          {{ loginError }}
        </div>

        <!-- OIDC Login -->
        <button
          v-if="auth.authMode === 'both' || auth.authMode === 'oidc'"
          class="glow-ring group flex w-full items-center justify-center gap-2 rounded-xl bg-accent px-4 py-2.5 text-[13px] font-semibold text-white transition-all duration-200 hover:bg-accent-hover hover:shadow-lg hover:shadow-accent/25 active:scale-[0.98]"
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
          <span class="mx-3 text-[10px] font-semibold uppercase tracking-[0.15em] text-text-muted">or</span>
          <div class="flex-grow border-t border-border-subtle"></div>
        </div>

        <!-- Static Token Login -->
        <form @submit.prevent="handleTokenLogin" class="space-y-4">
          <div>
            <label for="token" class="mb-1.5 block text-[12px] font-medium uppercase tracking-wider text-text-muted">Bearer Token</label>
            <div
              class="glow-ring flex items-center gap-2 rounded-xl border bg-surface-overlay/80 px-3 py-2.5 transition-all duration-200"
              :class="inputFocused ? 'active border-accent/40' : 'border-border-default'"
            >
              <KeyRound class="h-4 w-4 shrink-0 text-text-muted transition-colors duration-200" :class="{ 'text-accent': inputFocused }" :stroke-width="1.75" />
              <input
                id="token"
                v-model="tokenInput"
                type="password"
                placeholder="Enter your token"
                class="w-full bg-transparent font-mono text-[13px] text-text-primary placeholder-text-muted outline-none"
                @focus="inputFocused = true"
                @blur="inputFocused = false"
              />
            </div>
          </div>
          <button
            type="submit"
            :disabled="!tokenInput || auth.loading"
            class="group flex w-full items-center justify-center gap-2 rounded-xl border border-border-default bg-surface-overlay px-4 py-2.5 text-[13px] font-semibold text-text-primary transition-all duration-200 hover:border-accent/30 hover:bg-surface-hover active:scale-[0.98] disabled:pointer-events-none disabled:opacity-30"
          >
            <Loader2
              v-if="auth.loading"
              class="h-4 w-4 animate-spin text-accent"
              :stroke-width="2"
            />
            <KeyRound
              v-else
              class="h-4 w-4 text-text-muted transition-colors duration-200 group-hover:text-accent"
              :stroke-width="1.75"
            />
            {{ auth.loading ? 'Signing in...' : 'Sign in with Token' }}
          </button>
        </form>
      </div>

      <!-- Subtle branding below card -->
      <p class="mt-6 text-center text-[10px] tracking-[0.2em] text-text-muted/40">KEDGE COMMAND CENTER</p>
    </div>
  </div>
</template>
