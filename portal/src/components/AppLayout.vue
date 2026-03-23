<script setup lang="ts">
import AppSidebar from './AppSidebar.vue'
import AppHeader from './AppHeader.vue'
</script>

<template>
  <div class="flex h-screen bg-surface">
    <AppSidebar />
    <div class="flex flex-1 flex-col overflow-hidden">
      <AppHeader />
      <main class="dot-grid relative flex-1 overflow-y-auto p-6">
        <!-- Ambient glow -->
        <div class="pointer-events-none absolute right-0 top-0 h-64 w-64 rounded-full bg-accent/3 blur-[100px]" />
        <div class="pointer-events-none absolute bottom-0 left-1/3 h-48 w-48 rounded-full bg-success/3 blur-[80px]" />
        <div class="relative z-10">
          <slot />
        </div>
      </main>
    </div>
  </div>
</template>
