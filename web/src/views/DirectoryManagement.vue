<template>
  <div class="directory-management-page">
    <div class="page-header">
      <div class="header-content">
        <h1>{{ $t('asset.dirManagement') }}</h1>
        <p class="description">
          {{ $t('asset.dirScanHint') }}
        </p>
      </div>
    </div>
    <DirScanView />
  </div>
</template>

<script setup>
import DirScanView from '@/components/asset/DirScanView.vue'
</script>

<style scoped>
.directory-management-page {
  padding: 24px;
  background: hsl(var(--background));
  min-height: 100vh;
}

.page-header {
  margin-bottom: 24px;
  
  h1 {
    font-size: 28px;
    font-weight: 600;
    color: hsl(var(--foreground));
    margin: 0 0 8px 0;
  }
  
  .description {
    color: hsl(var(--muted-foreground));
    font-size: 14px;
    margin: 0;
  }
}
</style>
