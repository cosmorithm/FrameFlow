<script setup lang="ts">
import { ref } from 'vue'
import { api, reason } from './api'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
const emit = defineEmits<{ (e: 'close'): void; (e: 'published', id: number): void }>()
const file = ref<File | null>(null),
  caption = ref(''),
  tags = ref(''),
  err = ref(''),
  busy = ref(false)
function selected(e: Event) {
  file.value = (e.target as HTMLInputElement).files?.[0] || null
}
async function submit() {
  if (busy.value || !file.value) return
  busy.value = true
  err.value = ''
  try {
    const data = new FormData()
    data.append('file', file.value)
    data.append('caption', caption.value.trim())
    data.append(
      'tags',
      tags.value
        .split(/[,，\s]+/)
        .map((t) => t.replace(/^#+/, '').trim())
        .filter(Boolean)
        .join(','),
    )
    const res = await api<{ id: number }>('/videos', 'POST', data)
    emit('published', res.id)
  } catch (e) {
    err.value = reason(e)
  } finally {
    busy.value = false
  }
}
</script>
<template>
  <Modal title="发布视频" @close="!busy && emit('close')">
    <form class="form-body" @submit.prevent="submit">
      <label class="file-field"
        ><Icon name="upload" :size="27" /><strong>{{
          file ? file.name : '选择 MP4 视频（最大 64 MB）'
        }}</strong
        ><input type="file" accept=".mp4,video/mp4" required @change="selected"
      /></label>
      <label
        >作品说明<textarea
          v-model="caption"
          rows="3"
          maxlength="1000"
          placeholder="给这个片段写点什么…"
        ></textarea>
      </label>
      <label>标签<input v-model="tags" placeholder="用逗号或空格分开多个标签" /></label>
      <p class="form-error" v-if="err" role="alert">{{ err }}</p>
      <button class="primary" :disabled="busy || !file">{{ busy ? '上传中…' : '发布视频' }}</button>
    </form>
  </Modal>
</template>
