<script setup lang="ts">
import { Hexagon, ArrowLeft } from 'lucide-vue-next'
</script>

<template>
  <div class="dot-grid relative flex min-h-screen items-center justify-center bg-surface">
    <div class="pointer-events-none fixed inset-0 overflow-hidden">
      <div class="absolute top-1/3 left-1/2 h-60 w-60 -translate-x-1/2 rounded-full bg-accent/4 blur-[100px]" />
    </div>

    <div class="relative text-center">
      <div class="relative mx-auto flex h-16 w-16 items-center justify-center">
        <div class="absolute inset-0 rounded-2xl bg-accent/10 blur-md" />
        <div class="relative flex h-16 w-16 items-center justify-center rounded-2xl border border-border-subtle bg-surface-raised">
          <Hexagon class="h-8 w-8 text-text-muted/20" :stroke-width="1.25" />
        </div>
      </div>
      <h1 class="text-gradient mt-5 text-6xl font-bold tracking-tight">404</h1>
      <p class="mt-2 text-[13px] text-text-muted">Page not found</p>
      <router-link
        to="/"
        class="glow-ring mt-6 inline-flex items-center gap-1.5 rounded-xl border border-border-subtle bg-surface-raised px-4 py-2 text-[13px] font-medium text-text-secondary transition-all duration-150 hover:border-accent/30 hover:text-text-primary"
      >
        <ArrowLeft class="h-3.5 w-3.5" :stroke-width="1.75" />
        Go to Dashboard
      </router-link>
    </div>
  </div>
</template>
