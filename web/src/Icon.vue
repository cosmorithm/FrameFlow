<script setup lang="ts">
defineProps<{ name: string; size?: number; filled?: boolean }>()
const paths: Record<string, string> = {
  discover: 'M3 6.5h18v13H3z M3 6.5l3-3h15l-3 3 M8 3.5l-3 3 M14 3.5l-3 3 M11 10l4 2.8-4 2.8z',
  heart:
    'M20.8 4.8a5.3 5.3 0 0 0-7.5 0L12 6.2l-1.3-1.4a5.3 5.3 0 0 0-7.5 7.5L12 21l8.8-8.7a5.3 5.3 0 0 0 0-7.5z',
  bookmark: 'M6.5 4.5A1.5 1.5 0 0 1 8 3h8a1.5 1.5 0 0 1 1.5 1.5v16L12 17l-5.5 3.5z',
  comment:
    'M20.5 11.5a8.5 8.5 0 0 1-8.6 8.4c-1.5 0-2.8-.3-4-.9l-4.5 1.4 1.3-4.3a8.2 8.2 0 0 1-1.4-4.6 8.6 8.6 0 0 1 17.2 0z',
  plus: 'M12 5v14 M5 12h14',
  check: 'm5 12 4.5 4.5L19 7',
  x: 'm6 6 12 12 M18 6 6 18',
  left: 'm14.5 5-7 7 7 7',
  right: 'm9.5 5 7 7-7 7',
  up: 'm5 14 7-7 7 7',
  down: 'm5 10 7 7 7-7',
  play: 'M7 4.5 20 12 7 19.5z',
  pause: 'M8 5v14 M16 5v14',
  sound: 'M3.5 9.5h4l4.5-4v13l-4.5-4h-4z M15.5 8.4a5 5 0 0 1 0 7.2 M18.5 5.5a9 9 0 0 1 0 13',
  mute: 'M3.5 9.5h4l4.5-4v13l-4.5-4h-4z M16 9l5 6 M21 9l-5 6',
  expand: 'M3.7 9V4h5 M15.3 4h5v5 M20.3 15v5h-5 M8.7 20h-5v-5',
  eye: 'M2 12s3.5-6.3 10-6.3S22 12 22 12s-3.5 6.3-10 6.3S2 12 2 12z M14.4 12a2.4 2.4 0 1 1-4.8 0 2.4 2.4 0 1 1 4.8 0',
  upload: 'M12 15V3 M7 8l5-5 5 5 M4 14v5a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-5',
  user: 'M15.5 8a3.5 3.5 0 1 1-7 0 3.5 3.5 0 1 1 7 0 M4.5 20v-1a7.5 7.5 0 0 1 15 0v1',
  hash: 'M10 3 7 21 M17 3l-3 18 M4.5 8.5h15 M3.5 15.5h15',
  arrow: 'M4 12h15 M14 6l6 6-6 6',
  film: 'M4 4h16v16H4z M8 4v16 M16 4v16 M4 8h4 M4 12h4 M4 16h4 M16 8h4 M16 12h4 M16 16h4',
  queue: 'M4 6h16 M4 12h16 M4 18h11',
  logout: 'M10 4H5v16h5 M10 12h11 M17 8l4 4-4 4',
  replay: 'M3.5 10A8.5 8.5 0 1 1 5 17 M3.5 4v6h6',
}
</script>
<template>
  <svg
    :width="size || 20"
    :height="size || 20"
    viewBox="0 0 24 24"
    aria-hidden="true"
    :fill="filled ? 'currentColor' : 'none'"
    stroke="currentColor"
    stroke-width="1.65"
    stroke-linecap="round"
    stroke-linejoin="round"
  >
    <path :d="paths[name]" />
  </svg>
</template>
