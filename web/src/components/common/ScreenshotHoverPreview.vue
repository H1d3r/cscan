<template>
  <span
    class="screenshot-hover-trigger"
    @mouseenter="showPreview"
    @mouseleave="hidePreview"
    @click.stop
  >
    <slot />
  </span>

  <Teleport to="body">
    <div
      v-if="visible"
      class="screenshot-hover-preview"
      :style="previewStyle"
      aria-hidden="true"
    >
      <img :src="src" :alt="alt" @error="hidePreviewNow">
    </div>
  </Teleport>
</template>

<script setup>
import { onBeforeUnmount, ref } from 'vue'

const props = defineProps({
  src: { type: String, required: true },
  alt: { type: String, default: '' },
})

const visible = ref(false)
const previewStyle = ref({})
let hideTimer = null

function showPreview(event) {
  if (hideTimer) clearTimeout(hideTimer)

  const { innerWidth: viewportWidth, innerHeight: viewportHeight } = window
  const rect = event.currentTarget.getBoundingClientRect()
  const width = Math.min(Math.max(Math.floor(viewportWidth * 0.5), 280), 960, viewportWidth - 32)
  const height = Math.min(Math.max(Math.floor(viewportHeight * 0.5), 200), 620, viewportHeight - 32)
  const left = Math.max(16, rect.left - width - 16)
  const top = Math.min(Math.max(16, rect.top), Math.max(16, viewportHeight - height - 16))

  previewStyle.value = {
    width: `${width}px`,
    height: `${height}px`,
    left: `${left}px`,
    top: `${top}px`,
  }
  visible.value = true
}

function hidePreview() {
  hideTimer = setTimeout(hidePreviewNow, 80)
}

function hidePreviewNow() {
  if (hideTimer) clearTimeout(hideTimer)
  hideTimer = null
  visible.value = false
}

onBeforeUnmount(hidePreviewNow)
</script>

<style scoped lang="scss">
.screenshot-hover-trigger {
  display: inline-flex;
}

.screenshot-hover-preview {
  position: fixed;
  z-index: 4000;
  box-sizing: border-box;
  padding: 8px;
  pointer-events: none;
  background: var(--el-bg-color, #fff);
  border: 1px solid var(--el-border-color-light, #dcdfe6);
  border-radius: 8px;
  box-shadow: var(--el-box-shadow-light, 0 8px 24px rgba(0, 0, 0, 0.18));

  img {
    display: block;
    width: 100%;
    height: 100%;
    object-fit: contain;
  }
}
</style>
