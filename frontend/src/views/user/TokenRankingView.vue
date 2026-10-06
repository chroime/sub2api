<template>
  <AppLayout>
    <div class="mx-auto max-w-7xl space-y-6">
      <div class="card p-6">
        <UserTokenRanking />
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import AppLayout from '@/components/layout/AppLayout.vue'
import UserTokenRanking from '@/components/user/UserTokenRanking.vue'
</script>
