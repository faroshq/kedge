<script setup lang="ts">
import { Hexagon, ArrowLeft } from 'lucide-vue-next'
</script>

<template>
  <div class="flex min-h-screen items-center justify-center bg-surface">
    <div class="text-center">
      <div class="mx-auto flex h-16 w-16 items-center justify-center rounded-2xl bg-surface-raised">
        <Hexagon class="h-8 w-8 text-text-muted/30" :stroke-width="1.25" />
      </div>
      <h1 class="mt-4 text-5xl font-bold tracking-tight text-text-muted/20">404</h1>
      <p class="mt-2 text-[13px] text-text-muted">Page not found</p>
      <router-link
        to="/"
        class="mt-6 inline-flex items-center gap-1.5 rounded-lg bg-surface-raised px-4 py-2 text-[13px] font-medium text-text-secondary transition-all duration-150 hover:bg-surface-hover hover:text-text-primary"
      >
        <ArrowLeft class="h-3.5 w-3.5" :stroke-width="1.75" />
        Go to Dashboard
      </router-link>
    </div>
  </div>
</template>
