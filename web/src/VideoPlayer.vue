<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api, clock, count, type User, type Video } from './api'
import Icon from './Icon.vue'

const props = defineProps<{
  item: Video
  user: User | null
  muted: boolean
  suspended: boolean
  busy: boolean
}>()
const emit = defineEmits<{
  (e: 'mute' | 'like' | 'favorite' | 'follow' | 'comments' | 'next' | 'previous'): void
  (e: 'filter', tag: string): void
  (e: 'error', message: string): void
}>()
const stage = ref<HTMLElement | null>(null),
  media = ref<HTMLVideoElement | null>(null)
const playing = ref(false),
  ended = ref(false),
  failed = ref(false),
  current = ref(0),
  duration = ref(0)
const progress = computed(() => (duration.value > 0 ? (current.value / duration.value) * 100 : 0))
const interact = computed(() => props.item.interaction_available !== false)
const fullscreenSupported = typeof document.documentElement.requestFullscreen === 'function'
let alive = true,
  attemptedAuto = false,
  resumeAfterDialog = false,
  onScreen = true
let observer: IntersectionObserver | undefined
let touchX = 0,
  touchY = 0,
  wheelTime = 0,
  progressAt = -1,
  wallAt = 0,
  watched = 0
let token = '',
  startPending = false,
  fivePending = false,
  fiveSent = false,
  nextStartTry = 0,
  nextFiveTry = 0,
  visit = 0
function active() {
  const v = media.value
  return (
    !!v &&
    playing.value &&
    !v.paused &&
    !v.ended &&
    !v.seeking &&
    onScreen &&
    !document.hidden &&
    !props.suspended
  )
}
function baseline() {
  progressAt = media.value?.currentTime ?? -1
  wallAt = performance.now()
}
function resetVisit() {
  visit++
  token = ''
  startPending = false
  fivePending = false
  fiveSent = false
  nextStartTry = 0
  nextFiveTry = 0
  watched = 0
  baseline()
}
async function advanceStats() {
  if (!props.user || (!active() && watched < 5) || !alive) return
  const session = visit
  if (!token && !startPending && Date.now() >= nextStartTry) {
    startPending = true
    try {
      const res = await api<{ play_id: string }>('/videos/' + props.item.id + '/plays', 'POST')
      if (alive && session === visit) token = res.play_id
    } catch {
      if (session === visit) nextStartTry = Date.now() + 2500
    } finally {
      if (session === visit) startPending = false
    }
  }
  if (
    alive &&
    session === visit &&
    token &&
    watched >= 5 &&
    !fivePending &&
    !fiveSent &&
    Date.now() >= nextFiveTry
  ) {
    fivePending = true
    try {
      await api(
        '/videos/' + props.item.id + '/plays/' + encodeURIComponent(token) + '/five-seconds',
        'POST',
      )
      if (alive && session === visit) fiveSent = true
    } catch {
      if (session === visit) nextFiveTry = Date.now() + 1600
    } finally {
      if (session === visit) fivePending = false
    }
  }
}
function tick() {
  const v = media.value
  if (!v) return
  current.value = v.currentTime
  const now = performance.now()
  if (playing.value && !v.seeking && onScreen && !document.hidden && progressAt >= 0 && wallAt > 0) {
    const elapsed = (now - wallAt) / 1000,
      moved = v.currentTime - progressAt
    if (moved > 0 && elapsed > 0 && moved <= elapsed * v.playbackRate + 0.3)
      watched += moved / v.playbackRate
  }
  progressAt = v.currentTime
  wallAt = now
  void advanceStats()
}
function onPlaying() {
  playing.value = true
  ended.value = false
  baseline()
  void advanceStats()
}
function onPause() {
  tick()
  playing.value = false
  progressAt = -1
}
function onEnd() {
  tick()
  playing.value = false
  ended.value = true
  current.value = duration.value
  progressAt = -1
}
function autoPlay() {
  const v = media.value
  if (!v || props.suspended || document.hidden || failed.value || attemptedAuto) return
  attemptedAuto = true
  v.muted = props.muted
  void v.play().catch(() => {
    playing.value = false
  })
}
function metadata() {
  duration.value = media.value?.duration || 0
  autoPlay()
}
function toggle() {
  const v = media.value
  if (!v || props.suspended) return
  if (failed.value) {
    failed.value = false
    attemptedAuto = false
    v.load()
    return
  }
  if (v.paused || v.ended) {
    if (v.ended) {
      v.currentTime = 0
      resetVisit()
    }
    void v.play().catch(() => emit('error', '浏览器未能播放此视频。'))
  } else v.pause()
}
function seek(event: Event) {
  const v = media.value
  if (v && duration.value > 0) {
    progressAt = -1
    v.currentTime = Number((event.target as HTMLInputElement).value)
  }
}
function toggleFullscreen() {
  if (document.fullscreenElement) void document.exitFullscreen().catch(() => {})
  else void stage.value?.requestFullscreen?.().catch(() => emit('error', '浏览器暂不支持此操作。'))
}
function onVisibility() {
  if (document.hidden) {
    media.value?.pause()
    progressAt = -1
  }
}
function touchStart(e: TouchEvent) {
  touchX = e.touches[0]?.clientX ?? 0
  touchY = e.touches[0]?.clientY ?? 0
}
function touchEnd(e: TouchEvent) {
  if ((e.target as Element).closest('button,input')) return
  const dx = (e.changedTouches[0]?.clientX ?? touchX) - touchX,
    dy = (e.changedTouches[0]?.clientY ?? touchY) - touchY
  if (Math.abs(dy) > 62 && Math.abs(dy) > Math.abs(dx) * 1.4) emit(dy < 0 ? 'next' : 'previous')
}
function onWheel(e: WheelEvent) {
  if (Math.abs(e.deltaY) < 38 || Date.now() - wheelTime < 800 || props.suspended) return
  wheelTime = Date.now()
  emit(e.deltaY > 0 ? 'next' : 'previous')
}
watch(
  () => props.suspended,
  (s) => {
    if (s) {
      resumeAfterDialog = !!media.value && !media.value.paused
      media.value?.pause()
    } else if (resumeAfterDialog && !document.hidden) {
      resumeAfterDialog = false
      void media.value?.play().catch(() => {})
    }
  },
)
watch(
  () => props.muted,
  (m) => {
    if (media.value) media.value.muted = m
  },
)
watch(
  () => props.user?.id,
  () => {
    resetVisit()
    void advanceStats()
  },
)
onMounted(() => {
  document.addEventListener('visibilitychange', onVisibility)
  if ('IntersectionObserver' in window && stage.value) {
    observer = new IntersectionObserver(
      ([entry]) => {
        onScreen = !!entry && entry.intersectionRatio >= 0.35
        if (!onScreen) {
          media.value?.pause()
          progressAt = -1
        }
      },
      { threshold: [0, 0.35] },
    )
    observer.observe(stage.value)
  }
  autoPlay()
})
onBeforeUnmount(() => {
  alive = false
  visit++
  observer?.disconnect()
  document.removeEventListener('visibilitychange', onVisibility)
  media.value?.pause()
})
defineExpose({ toggle })
function mediaFailed() {
  failed.value = true
  playing.value = false
}
</script>
<template>
  <section
    ref="stage"
    class="stage"
    aria-label="视频播放器"
    @touchstart.passive="touchStart"
    @touchend.passive="touchEnd"
    @wheel="onWheel"
  >
    <video
      ref="media"
      :src="item.video_url"
      :poster="item.cover_url || undefined"
      :muted="muted"
      playsinline
      preload="metadata"
      @loadedmetadata="metadata"
      @durationchange="metadata"
      @playing="onPlaying"
      @timeupdate="tick"
      @pause="onPause"
      @ended="onEnd"
      @waiting="progressAt = -1"
      @seeking="progressAt = -1"
      @seeked="baseline"
      @error="mediaFailed"
      @click="toggle"
    ></video>
    <div class="stage-shade"></div>
    <span v-if="failed" class="media-error">视频暂时无法播放</span>
    <button
      v-if="!playing && !suspended"
      class="big-play"
      type="button"
      :aria-label="failed ? '重新加载视频' : ended ? '重新播放' : '播放视频'"
      @click="toggle"
    >
      <Icon :name="failed || ended ? 'replay' : 'play'" :size="28" :filled="!failed && !ended" />
    </button>
    <div class="video-details">
      <div class="byline">
        <span class="avatar">{{ Array.from(item.author_name || '帧')[0] }}</span>
        <strong>{{ item.author_name }}</strong>
        <button
          v-if="user?.id !== item.author_id"
          class="follow"
          :class="{ chosen: item.following }"
          :disabled="busy || !interact"
          :aria-pressed="item.following"
          @click="emit('follow')"
        >
          <Icon :name="item.following ? 'check' : 'plus'" :size="13" />{{
            item.following ? '已关注' : '关注'
          }}
        </button>
      </div>
      <h2>{{ item.caption || '无标题视频' }}</h2>
      <div v-if="item.tags?.length" class="video-tags">
        <button v-for="tag in item.tags" :key="tag" @click="emit('filter', tag)">#{{ tag }}</button>
      </div>
    </div>
    <div class="action-rail" aria-label="视频互动">
      <button
        :class="{ chosen: item.liked }"
        :disabled="busy || !interact"
        :aria-label="item.liked ? '取消点赞' : '点赞'"
        :aria-pressed="item.liked"
        @click="emit('like')"
      >
        <span><Icon name="heart" :filled="item.liked" /></span
        ><small>{{ interact ? count(item.like_count) : '—' }}</small>
      </button>
      <button :disabled="!interact" aria-label="打开评论" @click="emit('comments')">
        <span><Icon name="comment" /></span
        ><small>{{ interact ? count(item.comment_count) : '—' }}</small>
      </button>
      <button
        :class="{ chosen: item.favorited }"
        :disabled="busy || !interact"
        :aria-label="item.favorited ? '取消收藏' : '收藏'"
        :aria-pressed="item.favorited"
        @click="emit('favorite')"
      >
        <span><Icon name="bookmark" :filled="item.favorited" /></span
        ><small>{{ item.favorited ? '已收藏' : '收藏' }}</small>
      </button>
    </div>
    <div class="control-bar">
      <button @click="toggle" :aria-label="playing ? '暂停' : '播放'">
        <Icon :name="playing ? 'pause' : 'play'" :filled="!playing" :size="16" />
      </button>
      <span class="time">{{ clock(current) }}</span>
      <input
        type="range"
        aria-label="播放进度"
        min="0"
        :max="duration || 1"
        step=".01"
        :value="current"
        :style="{ '--progress': progress + '%' }"
        @input="seek"
      />
      <span class="time">{{ clock(duration) }}</span>
      <button @click="emit('mute')" :aria-label="muted ? '开启声音' : '静音'">
        <Icon :name="muted ? 'mute' : 'sound'" :size="18" />
      </button>
      <button v-if="fullscreenSupported" class="expand" @click="toggleFullscreen" aria-label="全屏">
        <Icon name="expand" :size="16" />
      </button>
    </div>
  </section>
</template>
