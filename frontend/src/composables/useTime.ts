import { ref, onUnmounted } from 'vue'

/** Returns a relative time string like "3m ago" */
export function relativeTime(dateStr: string): string {
  const now = Date.now()
  const then = new Date(dateStr).getTime()
  const diff = now - then
  if (diff < 0) return 'just now'
  const seconds = Math.floor(diff / 1000)
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  return `${days}d ago`
}

export function formatFullDate(dateStr: string): string {
  return new Date(dateStr).toLocaleString()
}

/** Returns freshness tier for visual indicator */
export function freshness(dateStr: string): 'live' | 'recent' | 'normal' | 'stale' {
  const diff = Date.now() - new Date(dateStr).getTime()
  if (diff < 60_000) return 'live'      // < 1 min
  if (diff < 300_000) return 'recent'   // < 5 min
  if (diff < 1_800_000) return 'normal' // < 30 min
  return 'stale'
}

/** Formats a wait time in seconds to a human-readable string */
export function formatWaitTime(seconds: number): string {
  if (seconds < 60) return `${Math.round(seconds)}s`
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`
  return `${(seconds / 3600).toFixed(1)}h`
}

/** Composable that returns time utility functions */
export function useTime() {
  return { relativeTime, formatFullDate, freshness, formatWaitTime }
}

/**
 * Composable that returns a ref auto-updated every 10 seconds
 * with the current relative time string for the given date.
 */
export function useAutoRefreshTime(dateStr: string) {
  const display = ref(relativeTime(dateStr))

  const timer = setInterval(() => {
    display.value = relativeTime(dateStr)
  }, 10_000)

  onUnmounted(() => clearInterval(timer))

  return display
}
