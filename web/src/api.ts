export interface User {
  id: number
  nickname: string
  email: string
}
export interface Video {
  id: number
  author_id: number
  author_name: string
  caption: string
  video_url: string
  cover_url: string
  tags: string[]
  view_count: number
  view_5s_count: number
  like_count: number
  comment_count: number
  liked: boolean
  favorited: boolean
  following: boolean
  published_at: string
  interaction_available: boolean
}
export interface Comment {
  id: number
  author_id: number
  author_name: string
  parent_id: number
  root_id: number
  content: string
  created_at: string
  like_count: number
  liked: boolean
  reply_count: number
}
export interface Page<T> {
  items: T[]
  next_cursor: string
}

export class ApiError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message)
  }
}

export async function api<T>(path: string, method = 'GET', body?: object | FormData): Promise<T> {
  const form = body instanceof FormData
  let response: Response
  try {
    response = await fetch('/api' + path, {
      method,
      credentials: 'same-origin',
      headers: body && !form ? { 'Content-Type': 'application/json' } : undefined,
      body: form ? body : body ? JSON.stringify(body) : undefined,
    })
  } catch {
    throw new ApiError('连接中断，请检查网络后重试。', 0)
  }
  if (!response.ok) {
    const result = await response.json().catch(() => ({}))
    throw new ApiError(result.error || '请求失败，请稍后重试。', response.status)
  }
  if (response.status === 204) return undefined as T
  return (await response.json()) as T
}

export function reason(error: unknown): string {
  return error instanceof Error ? error.message : '操作失败，请重试。'
}
export function count(value: number) {
  return value >= 10000 ? (value / 10000).toFixed(1).replace(/\.0$/, '') + '万' : String(value)
}
export function clock(value: number) {
  if (!Number.isFinite(value)) return '0:00'
  return Math.floor(value / 60) + ':' + String(Math.floor(value % 60)).padStart(2, '0')
}
export function day(value: string) {
  const d = new Date(value)
  return Number.isNaN(+d) ? '' : d.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' })
}
