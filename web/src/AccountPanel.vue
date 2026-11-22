<script setup lang="ts">
import { ref } from 'vue'
import { api, reason, type User } from './api'
import Modal from './Modal.vue'
const props = defineProps<{ user: User | null }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'change', user: User | null): void }>()
const mode = ref<'login' | 'register'>('login'),
  email = ref(''),
  password = ref(''),
  nickname = ref(''),
  err = ref(''),
  busy = ref(false)
async function submit() {
  if (busy.value) return
  busy.value = true
  err.value = ''
  try {
    if (props.user) {
      await api('/logout', 'POST')
      emit('change', null)
    } else {
      const path = mode.value === 'register' ? '/register' : '/login'
      const body =
        mode.value === 'register'
          ? { email: email.value.trim(), password: password.value, nickname: nickname.value.trim() }
          : { email: email.value.trim(), password: password.value }
      const data = await api<{ user: User }>(path, 'POST', body)
      emit('change', data.user)
    }
  } catch (e) {
    err.value = reason(e)
  } finally {
    busy.value = false
  }
}
function switchMode() {
  mode.value = mode.value === 'login' ? 'register' : 'login'
  err.value = ''
}
</script>
<template>
  <Modal :title="user ? '我的账号' : mode === 'login' ? '登录帧流' : '创建账号'" @close="emit('close')">
    <div v-if="user" class="form-body">
      <strong>{{ user.nickname }}</strong
      ><span>{{ user.email }}</span>
      <p v-if="err" class="form-error" role="alert">{{ err }}</p>
      <button class="primary" :disabled="busy" @click="submit">退出登录</button>
    </div>
    <form v-else class="form-body" @submit.prevent="submit">
      <label v-if="mode === 'register'"
        >昵称<input v-model="nickname" required maxlength="64" autocomplete="nickname"
      /></label>
      <label
        >邮箱<input
          v-model="email"
          type="email"
          required
          autocomplete="username"
          placeholder="name@example.com"
      /></label>
      <label
        >密码<input
          v-model="password"
          type="password"
          required
          :minlength="mode === 'register' ? 8 : 1"
          :autocomplete="mode === 'register' ? 'new-password' : 'current-password'"
          :placeholder="mode === 'register' ? '至少 8 位' : '输入密码'"
      /></label>
      <p v-if="err" class="form-error" role="alert">{{ err }}</p>
      <button class="primary" :disabled="busy">
        {{ busy ? '处理中…' : mode === 'login' ? '登录' : '注册并登录' }}
      </button>
      <button
        type="button"
        class="text-button"
        @click="switchMode"
      >
        {{ mode === 'login' ? '还没有账号？注册' : '已有账号？登录' }}
      </button>
    </form>
  </Modal>
</template>
