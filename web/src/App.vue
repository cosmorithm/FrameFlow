<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { api, ApiError, count, day, reason, type Page, type User, type Video } from './api'
import Icon from './Icon.vue'
import VideoPlayer from './VideoPlayer.vue'
import AccountPanel from './AccountPanel.vue'
import CommentPanel from './CommentPanel.vue'
import PublishPanel from './PublishPanel.vue'

type Section = 'feed' | 'likes' | 'favorites'
type Panel = 'account' | 'publish' | 'comments' | null
const tabs: { id: Section; title: string; icon: string }[] = [
  { id: 'feed', title: '发现', icon: 'discover' },
  { id: 'likes', title: '我赞过的', icon: 'heart' },
  { id: 'favorites', title: '我的收藏', icon: 'bookmark' },
]
const section = ref<Section>('feed'),
  tag = ref(''),
  videos = ref<Video[]>([]),
  idx = ref(0),
  cursor = ref(''),
  nextLoading = ref(false)
const user = ref<User | null>(null),
  ready = ref(false),
  loading = ref(false),
  loadError = ref(''),
  errorNext = ref(''),
  panel = ref<Panel>(null)
const pending = ref(false),
  notification = ref(''),
  muted = ref(true),
  player = ref<InstanceType<typeof VideoPlayer> | null>(null)
const current = computed(() => videos.value[idx.value]),
  title = computed(() => tabs.find((t) => t.id === section.value)?.title ?? '发现')
const canPrev = computed(() => idx.value > 0),
  canNext = computed(() => idx.value < videos.value.length - 1 || !!cursor.value)
const queue = ref<HTMLElement | null>(null)
let version = 0,
  toastTimer = 0,
  moveAt = 0

function show(message: string) {
  notification.value = message
  clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => (notification.value = ''), 3400)
}
function path(after?: string) {
  const search = new URLSearchParams({ limit: '8' })
  if (tag.value && section.value === 'feed') search.set('tag', tag.value)
  if (after) search.set('cursor', after)
  return '/' + section.value + '?' + search
}
async function refresh() {
  const versionAt = ++version
  idx.value = 0
  videos.value = []
  cursor.value = ''
  loading.value = true
  nextLoading.value = false
  errorNext.value = ''
  loadError.value = ''
  if (section.value !== 'feed' && !user.value) {
    loading.value = false
    return
  }
  try {
    const result = await api<Page<Video>>(path())
    if (versionAt !== version) return
    videos.value = result.items || []
    cursor.value = result.next_cursor || ''
  } catch (e) {
    if (versionAt === version) loadError.value = reason(e)
  } finally {
    if (versionAt === version) loading.value = false
  }
}
async function more() {
  if (!cursor.value || nextLoading.value) return
  nextLoading.value = true
  errorNext.value = ''
  const versionAt = version
  try {
    const result = await api<Page<Video>>(path(cursor.value))
    if (versionAt !== version) return
    const ids = new Set(videos.value.map((v) => v.id))
    videos.value.push(...(result.items || []).filter((v) => !ids.has(v.id)))
    cursor.value = result.next_cursor || ''
  } catch (e) {
    if (versionAt === version) errorNext.value = reason(e)
  } finally {
    if (versionAt === version) nextLoading.value = false
  }
}
function changeSection(id: Section) {
  if (section.value === id && !tag.value) return
  section.value = id
  tag.value = ''
  panel.value = null
  void refresh()
}
function filter(t: string) {
  section.value = 'feed'
  tag.value = t
  panel.value = null
  void refresh()
  window.scrollTo({ top: 0, behavior: 'smooth' })
}
function select(index: number) {
  if (index === idx.value || index < 0 || index >= videos.value.length) return
  idx.value = index
  panel.value = null
  void nextTick(() => {
    const row = queue.value?.querySelector<HTMLElement>('[aria-current="true"]')
    if (row && queue.value)
      queue.value.scrollTop = Math.max(0, row.offsetTop - queue.value.offsetTop - 12)
  })
}
async function move(direction: number) {
  if (panel.value || performance.now() - moveAt < 240) return
  if ((direction < 0 && !canPrev.value) || (direction > 0 && !canNext.value)) return
  moveAt = performance.now()
  const target = idx.value + direction
  if (target >= videos.value.length) {
    await more()
    if (errorNext.value) {
      show(errorNext.value)
      return
    }
  }
  select(target)
}
function login() {
  panel.value = 'account'
}
function publish() {
  panel.value = user.value ? 'publish' : 'account'
  if (!user.value) show('请先登录后发布视频。')
}
async function accountChanged(value: User | null) {
  user.value = value
  panel.value = null
  if (!value) {
    section.value = 'feed'
    tag.value = ''
  }
  await refresh()
  show(value ? '已登录 ' + value.nickname : '已退出登录')
}
async function published(id: number) {
  section.value = 'feed'
  tag.value = ''
  panel.value = null
  await refresh()
  const at = videos.value.findIndex((v) => v.id === id)
  if (at >= 0) select(at)
  show('视频已发布。')
}
async function mutate(type: 'like' | 'favorite' | 'follow') {
  if (!user.value) {
    login()
    return
  }
  const v = current.value
  if (pending.value || !v || !v.interaction_available) return
  pending.value = true
  try {
    const before = type === 'like' ? v.liked : type === 'favorite' ? v.favorited : v.following
    const url = type === 'follow' ? '/users/' + v.author_id + '/follow' : '/videos/' + v.id + '/' + type
    const { enabled } = await api<{ enabled: boolean }>(url, 'PUT', { enabled: !before })
    if (type === 'like') {
      v.liked = enabled
      if (enabled !== before) v.like_count = Math.max(0, v.like_count + (enabled ? 1 : -1))
    } else if (type === 'favorite') v.favorited = enabled
    else
      videos.value
        .filter((x) => x.author_id === v.author_id)
        .forEach((x) => {
          x.following = enabled
        })
    if (
      !enabled &&
      ((type === 'like' && section.value === 'likes') ||
        (type === 'favorite' && section.value === 'favorites'))
    ) {
      const at = videos.value.findIndex((x) => x.id === v.id)
      if (at >= 0) {
        videos.value.splice(at, 1)
        idx.value = Math.max(0, Math.min(idx.value, videos.value.length - 1))
      }
      if (!videos.value.length && cursor.value) await more()
    }
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) {
      user.value = null
      login()
    } else show(reason(e))
  } finally {
    pending.value = false
  }
}
function key(e: KeyboardEvent) {
  if (panel.value || e.defaultPrevented || e.ctrlKey || e.altKey || e.metaKey || e.shiftKey) return
  if ((e.target as Element)?.closest('button,input,select,textarea,a,[contenteditable]')) return
  if (['ArrowDown', 'ArrowRight', 'ArrowUp', 'ArrowLeft'].includes(e.key)) {
    e.preventDefault()
    void move(['ArrowDown', 'ArrowRight'].includes(e.key) ? 1 : -1)
  } else if (e.code === 'Space') {
    e.preventDefault()
    player.value?.toggle()
  }
}
onMounted(async () => {
  document.addEventListener('keydown', key)
  try {
    const result = await api<{ user: User | null }>('/me')
    user.value = result.user
  } catch (e) {
    show(reason(e))
  }
  ready.value = true
  await refresh()
})
onBeforeUnmount(() => {
  clearTimeout(toastTimer)
  document.removeEventListener('keydown', key)
})
function openHome() {
  section.value = 'feed'
  tag.value = ''
  panel.value = null
  void refresh()
}
</script>
<template>
  <header class="site-header">
    <div class="top-inner">
      <button
        class="brand"
        aria-label="帧流首页"
        @click="openHome"
      >
        <span class="brand-mark" aria-hidden="true"><i></i><i></i></span
        ><span>Frame<span class="brand-flow">Flow</span></span>
      </button>
      <nav aria-label="视频分类">
        <button
          v-for="t in tabs"
          :key="t.id"
          :aria-current="section === t.id ? 'page' : undefined"
          @click="changeSection(t.id)"
        >
          {{ t.title }}
        </button>
      </nav>
      <div class="account-actions">
        <button class="primary publish" @click="publish"><Icon name="plus" :size="17" />发布</button
        ><button
          class="account"
          :aria-label="user ? '账号：' + user.nickname : '邮箱登录'"
          @click="login"
        >
          <span v-if="user" class="account-avatar">{{ Array.from(user.nickname)[0] }}</span
          ><Icon v-else name="user" :size="20" /><span>{{ user ? user.nickname : '登录' }}</span>
        </button>
      </div>
    </div>
  </header>
  <main>
    <div class="page-head">
      <div class="page-titles">
        <h1>{{ title }}</h1>
        <button v-if="tag" class="filter-tag" :aria-label="'清除标签 ' + tag" @click="filter('')">
          # {{ tag }}<Icon name="x" :size="13" />
        </button>
      </div>
      <div class="switcher">
        <button aria-label="上一个视频" :disabled="!canPrev" @click="move(-1)">
          <Icon name="left" :size="15" /></button
        ><span v-if="current">{{ idx + 1 }} <i>/</i> {{ videos.length }}{{ cursor ? ' +' : '' }}</span
        ><button aria-label="下一个视频" :disabled="!canNext || nextLoading" @click="move(1)">
          <Icon name="right" :size="15" />
        </button>
      </div>
    </div>
    <div v-if="current" class="layout">
      <div class="screen-column">
        <VideoPlayer
          ref="player"
          :key="current.id"
          :item="current"
          :user="user"
          :muted="muted"
          :suspended="!!panel"
          :busy="pending"
          @mute="muted = !muted"
          @like="mutate('like')"
          @favorite="mutate('favorite')"
          @follow="mutate('follow')"
          @comments="panel = 'comments'"
          @next="move(1)"
          @previous="move(-1)"
          @filter="filter"
          @error="show"
        />
        <div class="screen-foot">
          <span><Icon name="eye" :size="16" />{{ count(current.view_count) }} 次浏览</span
          ><span>{{ day(current.published_at) }}发布</span
          ><span v-if="!current.interaction_available" class="warn">互动数据暂不可用</span>
        </div>
      </div>
      <aside class="playlist">
        <div class="playlist-head">
          <h2><Icon name="queue" :size="18" />播放列表</h2>
        </div>
        <div ref="queue" class="queue">
          <button
            v-for="(video, i) in videos"
            :key="video.id"
            :aria-current="i === idx ? 'true' : undefined"
            class="queue-row"
            @click="select(i)"
          >
            <span class="thumb"
              ><img v-if="video.cover_url" :src="video.cover_url" alt="" loading="lazy" /><Icon
                v-else
                name="film"
                :size="21" /><span v-if="i === idx" class="bars" aria-hidden="true"
                ><i></i><i></i><i></i></span
            ></span>
            <span class="queue-copy"
              ><span class="queue-title">{{ video.caption || '无标题视频' }}</span
              ><span class="queue-author">{{ video.author_name }}</span></span
            >
          </button>
        </div>
        <button v-if="cursor" class="load-more" :disabled="nextLoading" @click="more">
          {{ nextLoading ? '加载中…' : '加载更多' }}
        </button>
        <p v-if="errorNext" class="form-error" role="alert">{{ errorNext }}</p>
        <div class="key-tip">
          <kbd>↑</kbd><kbd>↓</kbd><span>切换视频</span><kbd>空格</kbd><span>播放 / 暂停</span>
        </div>
      </aside>
    </div>
    <section v-else class="blank" aria-live="polite">
      <template v-if="loading || !ready"
        ><span class="spinner"></span>
        <p>加载视频中…</p></template
      >
      <template v-else-if="loadError"
        ><Icon name="film" :size="32" />
        <h2>视频加载失败</h2>
        <p>{{ loadError }}</p>
        <button class="outline" @click="refresh">重试</button></template
      >
      <template v-else-if="user === null && section !== 'feed'"
        ><Icon name="user" :size="32" />
        <h2>登录后查看{{ section === 'likes' ? '点赞' : '收藏' }}的视频</h2>
        <button class="primary" @click="login">邮箱登录</button></template
      >
      <template v-else
        ><Icon
          :name="section === 'favorites' ? 'bookmark' : section === 'likes' ? 'heart' : 'film'"
          :size="32"
        />
        <h2>
          {{
            section === 'favorites'
              ? '还没有收藏的视频'
              : section === 'likes'
                ? '还没有赞过的视频'
                : '这里还没有视频'
          }}
        </h2>
        <button
          class="outline"
          @click="tag ? filter('') : section !== 'feed' ? changeSection('feed') : publish()"
        >
          {{ tag ? '清除标签' : section !== 'feed' ? '去发现' : '发布视频' }}
        </button></template
      >
    </section>
  </main>
  <AccountPanel v-if="panel === 'account'" :user="user" @close="panel = null" @change="accountChanged" />
  <PublishPanel v-if="panel === 'publish'" @close="panel = null" @published="published" />
  <CommentPanel
    v-if="panel === 'comments' && current"
    :video="current"
    :user="user"
    @close="panel = null"
    @login="login"
    @posted="current.comment_count++"
  />
  <div v-if="notification" class="toast" role="status">{{ notification }}</div>
</template>
