<script setup lang="ts">
import { nextTick, onMounted, ref } from 'vue'
import { api, count, day, reason, type Comment, type Page, type User, type Video } from './api'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
const props = defineProps<{ video: Video; user: User | null }>()
const emit = defineEmits<{ (e: 'close' | 'login' | 'posted'): void }>()
const roots = ref<Comment[]>([]),
  replies = ref<Record<number, Comment[]>>({}),
  open = ref<Record<number, boolean>>({})
const cursors = ref<Record<number, string>>({}),
  loading = ref<Record<number, boolean>>({}),
  likes = ref<Record<number, boolean>>({})
const content = ref(''),
  target = ref<Comment | null>(null),
  error = ref(''),
  sending = ref(false),
  textarea = ref<HTMLTextAreaElement | null>(null)
const base = '/videos/' + props.video.id + '/comments'
async function load(root = 0, append = false) {
  if (loading.value[root]) return
  loading.value[root] = true
  error.value = ''
  try {
    const params = new URLSearchParams()
    if (root) params.set('root_id', String(root))
    if (append && cursors.value[root]) params.set('cursor', cursors.value[root])
    const data = await api<Page<Comment>>(base + (params.size ? '?' + params : ''))
    const old = append ? (root ? replies.value[root] : roots.value) : []
    const map = new Map((old || []).map((c) => [c.id, c]))
    ;(data.items || []).forEach((c) => map.set(c.id, c))
    if (root) replies.value[root] = [...map.values()]
    else roots.value = [...map.values()]
    cursors.value[root] = data.next_cursor || ''
  } catch (e) {
    error.value = reason(e)
  } finally {
    loading.value[root] = false
  }
}
async function expand(id: number) {
  open.value[id] = !open.value[id]
  if (open.value[id] && !replies.value[id]) await load(id)
}
function reply(c: Comment) {
  if (!props.user) {
    emit('login')
    return
  }
  target.value = c
  void nextTick(() => textarea.value?.focus())
}
async function like(c: Comment) {
  if (!props.user) {
    emit('login')
    return
  }
  if (likes.value[c.id]) return
  likes.value[c.id] = true
  error.value = ''
  try {
    const enabled = !c.liked
    await api('/comments/' + c.id + '/like', 'PUT', { enabled })
    c.liked = enabled
    c.like_count = Math.max(0, c.like_count + (enabled ? 1 : -1))
  } catch (e) {
    error.value = reason(e)
  } finally {
    likes.value[c.id] = false
  }
}
async function send() {
  if (!props.user) {
    emit('login')
    return
  }
  if (!content.value.trim() || sending.value) return
  sending.value = true
  error.value = ''
  try {
    const parent = target.value
    const c = await api<Comment>(base, 'POST', {
      content: content.value.trim(),
      ...(parent ? { parent_id: parent.id } : {}),
    })
    content.value = ''
    target.value = null
    emit('posted')
    if (parent) {
      const root = parent.root_id || parent.id
      const top = roots.value.find((v) => v.id === root)
      if (top) top.reply_count++
      open.value[root] = true
      await load(root)
    } else {
      roots.value.unshift(c)
    }
  } catch (e) {
    error.value = reason(e)
  } finally {
    sending.value = false
  }
}
function author(c: Comment) {
  return c.author_name || '用户 ' + c.author_id
}
function parentName(c: Comment, r: Comment) {
  const p = (replies.value[r.id] || []).find((x) => x.id === c.parent_id)
  return p ? author(p) : c.parent_id === r.id ? author(r) : ''
}
onMounted(() => {
  void load()
})
</script>
<template>
  <Modal :title="'评论 · ' + video.comment_count" drawer @close="emit('close')">
    <div class="comments-list">
      <p v-if="loading[0] && !roots.length" class="muted-note">正在加载评论…</p>
      <p v-else-if="!roots.length && !error" class="muted-note">还没有评论，来写第一条吧。</p>
      <article v-for="r in roots" :key="r.id" class="comment">
        <div class="avatar">{{ Array.from(author(r))[0] }}</div>
        <div class="comment-body">
          <div class="comment-by">
            <b>{{ author(r) }}</b
            ><small>{{ day(r.created_at) }}</small>
          </div>
          <p>{{ r.content }}</p>
          <div class="comment-actions">
            <button @click="reply(r)">回复</button
            ><button
              :class="{ chosen: r.liked }"
              :disabled="likes[r.id]"
              :aria-label="r.liked ? '取消评论赞' : '赞评论'"
              @click="like(r)"
            >
              <Icon name="heart" :filled="r.liked" :size="13" />{{ count(r.like_count) }}
            </button>
          </div>
          <button v-if="r.reply_count > 0" class="reply-toggle" @click="expand(r.id)">
            {{ open[r.id] ? '收起回复' : '查看 ' + r.reply_count + ' 条回复'
            }}<Icon :name="open[r.id] ? 'up' : 'down'" :size="13" />
          </button>
          <div v-if="open[r.id]" class="replies">
            <div class="reply" v-for="c in replies[r.id] || []" :key="c.id">
              <div class="comment-by">
                <b>{{ author(c) }}</b
                ><small>{{ day(c.created_at) }}</small>
              </div>
              <p>
                <span v-if="c.parent_id !== r.id && parentName(c, r)" class="reply-to"
                  >回复 {{ parentName(c, r) }} </span
                >{{ c.content }}
              </p>
              <div class="comment-actions">
                <button @click="reply(c)">回复</button
                ><button
                  :class="{ chosen: c.liked }"
                  :disabled="likes[c.id]"
                  :aria-label="c.liked ? '取消回复赞' : '赞回复'"
                  @click="like(c)"
                >
                  <Icon name="heart" :size="13" :filled="c.liked" />{{ count(c.like_count) }}
                </button>
              </div>
            </div>
            <button
              v-if="cursors[r.id]"
              class="text-button"
              :disabled="loading[r.id]"
              @click="load(r.id, true)"
            >
              更多回复
            </button>
            <button v-else-if="!replies[r.id] && !loading[r.id]" class="text-button" @click="load(r.id)">
              重试加载回复
            </button>
          </div>
        </div>
      </article>
      <button v-if="cursors[0]" class="text-button" :disabled="loading[0]" @click="load(0, true)">
        更多评论
      </button>
    </div>
    <div class="comment-entry">
      <p v-if="error" class="form-error" role="alert">
        {{ error }} <button v-if="!roots.length" @click="load()">重试</button>
      </p>
      <form v-if="user" @submit.prevent="send">
        <div v-if="target" class="replying">
          回复 {{ author(target)
          }}<button type="button" @click="target = null" aria-label="取消回复">
            <Icon name="x" :size="13" />
          </button>
        </div>
        <textarea
          v-model="content"
          ref="textarea"
          rows="2"
          maxlength="1000"
          placeholder="写下评论…"
          aria-label="评论内容"
        ></textarea>
        <button class="primary" :disabled="sending || !content.trim()">
          {{ sending ? '发送中…' : '发送' }}<Icon name="arrow" :size="15" />
        </button>
      </form>
      <button v-else class="primary" @click="emit('login')">登录后评论</button>
    </div>
  </Modal>
</template>
