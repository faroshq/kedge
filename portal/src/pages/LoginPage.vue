<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { API_PATHS } from '@/lib/constants'

const auth = useAuthStore()
const router = useRouter()
const tokenInput = ref('')
const loginError = ref<string | null>(null)

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
  // Generate PKCE code_verifier
  const array = new Uint8Array(32)
  crypto.getRandomValues(array)
  const codeVerifier = btoa(String.fromCharCode(...array))
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '')

  const sessionId = crypto.randomUUID()

  // Store verifier for callback page
  sessionStorage.setItem('oidc_verifier', codeVerifier)
  sessionStorage.setItem('oidc_session', sessionId)

  // Build callback URL pointing back to portal
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
  <div class="flex min-h-screen items-center justify-center bg-gray-50">
    <div class="w-full max-w-sm space-y-6 rounded-lg border border-gray-200 bg-white p-8">
      <div class="text-center">
        <h1 class="text-2xl font-semibold text-gray-900">Kedge</h1>
        <p class="mt-1 text-sm text-gray-500">Sign in to your account</p>
      </div>

      <div v-if="loginError" class="rounded-md bg-red-50 p-3 text-sm text-red-700">
        {{ loginError }}
      </div>

      <!-- OIDC Login -->
      <button
        v-if="auth.authMode === 'both' || auth.authMode === 'oidc'"
        class="w-full rounded-md bg-gray-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-gray-800 focus:outline-none focus:ring-2 focus:ring-gray-500 focus:ring-offset-2"
        @click="handleOIDCLogin"
      >
        Sign in with SSO
      </button>

      <div
        v-if="auth.authMode === 'both'"
        class="relative flex items-center py-2"
      >
        <div class="flex-grow border-t border-gray-200"></div>
        <span class="mx-3 text-xs text-gray-400">or</span>
        <div class="flex-grow border-t border-gray-200"></div>
      </div>

      <!-- Static Token Login -->
      <form @submit.prevent="handleTokenLogin" class="space-y-4">
        <div>
          <label for="token" class="block text-sm font-medium text-gray-700">Bearer Token</label>
          <input
            id="token"
            v-model="tokenInput"
            type="password"
            placeholder="Enter your token"
            class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-gray-500 focus:outline-none focus:ring-1 focus:ring-gray-500"
          />
        </div>
        <button
          type="submit"
          :disabled="!tokenInput || auth.loading"
          class="w-full rounded-md border border-gray-300 bg-white px-4 py-2.5 text-sm font-medium text-gray-700 hover:bg-gray-50 focus:outline-none focus:ring-2 focus:ring-gray-500 focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {{ auth.loading ? 'Signing in...' : 'Sign in with Token' }}
        </button>
      </form>
    </div>
  </div>
</template>
