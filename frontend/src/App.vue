<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { SpaceSummary, KnowledgeSpace, TmuxAgentStatus, AgentUpdate } from '@/types'
import { api } from '@/api/client'
import { useSSE } from '@/composables/useSSE'

import { SidebarProvider, SidebarInset, SidebarTrigger } from '@/components/ui/sidebar'
import { Separator } from '@/components/ui/separator'
import { TooltipProvider } from '@/components/ui/tooltip'
import { Button } from '@/components/ui/button'
import AppSidebar from '@/components/AppSidebar.vue'
import SpaceOverview from '@/components/SpaceOverview.vue'
import AgentDetail from '@/components/AgentDetail.vue'
import EventLog from '@/components/EventLog.vue'
import { useTheme } from '@/composables/useTheme'

const { theme, toggle: toggleTheme } = useTheme()

// ── Router ─────────────────────────────────────────────────────────
const route = useRoute()
const router = useRouter()

// ── State ──────────────────────────────────────────────────────────
const spaces = ref<SpaceSummary[]>([])
const currentSpace = ref<KnowledgeSpace | null>(null)
const tmuxStatus = ref<Record<string, TmuxAgentStatus>>({})

const loading = ref(true)
const errorMessage = ref<string | null>(null)
const statusAnnouncement = ref('')

const sse = useSSE()
const eventLogRef = ref<InstanceType<typeof EventLog> | null>(null)
let pollTimer: ReturnType<typeof setInterval> | null = null

// ── Route-derived selection ────────────────────────────────────────
const selectedSpace = computed(() => {
  const p = route.params.space
  return typeof p === 'string' ? p : ''
})

const selectedAgent = computed(() => {
  const p = route.params.agent
  return typeof p === 'string' ? p : ''
})

// ── Computed ───────────────────────────────────────────────────────
const selectedAgentData = computed<AgentUpdate | null>(() => {
  if (!currentSpace.value || !selectedAgent.value) return null
  return currentSpace.value.agents[selectedAgent.value] ?? null
})

const selectedAgentTmux = computed<TmuxAgentStatus | null>(() => {
  if (!selectedAgent.value) return null
  return tmuxStatus.value[selectedAgent.value] ?? null
})

// ── Error feedback ────────────────────────────────────────────────
function showError(msg: string) {
  errorMessage.value = msg
  statusAnnouncement.value = `Error: ${msg}`
  setTimeout(() => {
    errorMessage.value = null
  }, 5000)
}

function showStatus(msg: string) {
  statusAnnouncement.value = msg
}

// ── Data fetching ──────────────────────────────────────────────────
async function loadSpaces() {
  try {
    const fetched = await api.fetchSpaces()
    // Sort by updated_at descending (newest first)
    fetched.sort((a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime())
    spaces.value = fetched
  } catch (err) {
    console.error('Failed to load spaces:', err)
    showError('Failed to load spaces. Check server connection.')
  }
}

async function loadSpace(name: string) {
  try {
    currentSpace.value = await api.fetchSpace(name)
  } catch (err) {
    console.error(`Failed to load space ${name}:`, err)
    currentSpace.value = null
    showError(`Failed to load space "${name}".`)
  }
}

async function loadTmuxStatus(space: string) {
  try {
    const raw = await api.fetchTmuxStatus(space)
    // The server returns an array of {agent, ...} objects — normalize to a map
    if (Array.isArray(raw)) {
      const map: Record<string, TmuxAgentStatus> = {}
      for (const item of raw as any[]) {
        if (item.agent) {
          map[item.agent] = item
        }
      }
      tmuxStatus.value = map
    } else {
      tmuxStatus.value = raw
    }
  } catch {
    tmuxStatus.value = {}
  }
}

// ── Selection handlers (via router) ────────────────────────────────
function handleSelectSpace(name: string) {
  router.push('/' + name)
}

function handleSelectAgent(name: string) {
  router.push('/' + selectedSpace.value + '/' + name)
}

// ── Watch route params for data loading & SSE ──────────────────────
watch(
  () => selectedSpace.value,
  (space, oldSpace) => {
    if (space && space !== oldSpace) {
      loadSpace(space)
      loadTmuxStatus(space)
      // Reconnect SSE to this space
      sse.disconnect()
      sse.connect(space)
    } else if (!space) {
      currentSpace.value = null
      tmuxStatus.value = {}
      sse.disconnect()
      sse.connect() // global SSE
    }
  },
)

// ── Action handlers ────────────────────────────────────────────────
async function handleBroadcastSpace() {
  if (!selectedSpace.value) return
  try {
    await api.broadcastSpace(selectedSpace.value)
    showStatus(`Broadcast sent to all agents in ${selectedSpace.value}`)
  } catch (err) {
    console.error('Broadcast failed:', err)
    showError('Broadcast failed. Please try again.')
  }
}

async function handleApproveAgent() {
  if (!selectedSpace.value || !selectedAgent.value) return
  try {
    await api.approveAgent(selectedSpace.value, selectedAgent.value)
    await loadTmuxStatus(selectedSpace.value)
    showStatus(`Approved ${selectedAgent.value}`)
  } catch (err) {
    console.error('Approve failed:', err)
    showError('Approval failed. Please try again.')
  }
}

async function handleReplyAgent(text: string) {
  if (!selectedSpace.value || !selectedAgent.value) return
  try {
    await api.replyToAgent(selectedSpace.value, selectedAgent.value, text)
    showStatus(`Reply sent to ${selectedAgent.value}`)
  } catch (err) {
    console.error('Reply failed:', err)
    showError('Reply failed. Please try again.')
  }
}

async function handleBroadcastAgent() {
  if (!selectedSpace.value || !selectedAgent.value) return
  try {
    await api.broadcastAgent(selectedSpace.value, selectedAgent.value)
    showStatus(`Broadcast sent to ${selectedAgent.value}`)
  } catch (err) {
    console.error('Broadcast agent failed:', err)
    showError('Broadcast failed. Please try again.')
  }
}

async function handleDismissQuestion(index: number) {
  if (!selectedSpace.value || !selectedAgent.value) return
  try {
    await api.dismissItem(selectedSpace.value, selectedAgent.value, index, 'question')
    await loadSpace(selectedSpace.value)
    showStatus('Question dismissed')
  } catch (err) {
    console.error('Dismiss question failed:', err)
    showError('Failed to dismiss question.')
  }
}

async function handleDismissBlocker(index: number) {
  if (!selectedSpace.value || !selectedAgent.value) return
  try {
    await api.dismissItem(selectedSpace.value, selectedAgent.value, index, 'blocker')
    await loadSpace(selectedSpace.value)
    showStatus('Blocker dismissed')
  } catch (err) {
    console.error('Dismiss blocker failed:', err)
    showError('Failed to dismiss blocker.')
  }
}

async function handleSendMessage(text: string, sender: string) {
  if (!selectedSpace.value || !selectedAgent.value) return
  try {
    await api.sendMessage(selectedSpace.value, selectedAgent.value, text, sender)
    await loadSpace(selectedSpace.value)
  } catch (err) {
    console.error('Send message failed:', err)
    showError('Failed to send message.')
  }
}

async function handleDeleteAgent(agentName?: string) {
  const space = selectedSpace.value
  const agent = agentName || selectedAgent.value
  if (!space || !agent) return
  try {
    await api.deleteAgent(space, agent)
    showStatus(`Deleted agent ${agent}`)
    // Navigate back to space overview if we deleted the currently selected agent
    if (agent === selectedAgent.value) {
      router.push('/' + space)
    }
    await loadSpace(space)
    await loadSpaces()
  } catch (err) {
    console.error('Delete agent failed:', err)
    showError(`Failed to delete agent "${agent}".`)
  }
}

async function handleBroadcastSingleAgent(agentName: string) {
  if (!selectedSpace.value) return
  try {
    await api.broadcastAgent(selectedSpace.value, agentName)
    showStatus(`Broadcast sent to ${agentName}`)
  } catch (err) {
    console.error('Broadcast agent failed:', err)
    showError('Broadcast failed. Please try again.')
  }
}

async function handleSendMessageToAgent(agentName: string, text: string) {
  if (!selectedSpace.value) return
  try {
    await api.sendMessage(selectedSpace.value, agentName, text, 'boss')
    await loadSpace(selectedSpace.value)
    showStatus(`Message sent to ${agentName}`)
  } catch (err) {
    console.error('Send message to agent failed:', err)
    showError(`Failed to send message to "${agentName}".`)
  }
}

// ── SSE event handlers ─────────────────────────────────────────────
function pushLog(type: string, msg: string) {
  eventLogRef.value?.pushSSEEvent(type, msg)
}

function setupSSE() {
  sse.on('agent_updated', (data) => {
    if (selectedSpace.value) {
      loadSpace(selectedSpace.value)
    }
    loadSpaces()
    statusAnnouncement.value = `Agent ${data.agent} updated: ${data.status}`
    pushLog('agent_updated', `[${data.agent}] ${data.status}: ${data.summary}`)
  })

  sse.on('agent_removed', (data) => {
    if (selectedSpace.value) {
      loadSpace(selectedSpace.value)
    }
    loadSpaces()
    statusAnnouncement.value = `Agent ${data.agent} removed`
    pushLog('agent_removed', `[${data.agent}] agent removed`)
  })

  sse.on('space_deleted', (spaceName) => {
    loadSpaces()
    statusAnnouncement.value = `Space ${spaceName} deleted`
    pushLog('space_deleted', `space "${spaceName}" deleted`)
    if (selectedSpace.value === spaceName) {
      currentSpace.value = null
      router.push('/')
    }
  })

  sse.on('tmux_liveness', (data) => {
    if (Array.isArray(data)) {
      const map: Record<string, TmuxAgentStatus> = { ...tmuxStatus.value }
      for (const item of data) {
        if (item.agent) {
          map[item.agent] = item as TmuxAgentStatus
        }
      }
      tmuxStatus.value = map
    }
    // tmux_liveness is high-frequency, don't spam the log
  })

  sse.on('agent_message', (data) => {
    if (selectedSpace.value) {
      loadSpace(selectedSpace.value)
    }
    pushLog('agent_message', `[${data.agent}] message from ${data.sender}`)
  })

  sse.on('broadcast_complete', () => {
    pushLog('broadcast_complete', 'Broadcast completed')
  })

  sse.on('broadcast_progress', (data) => {
    pushLog('broadcast_progress', data.message || 'Broadcast in progress...')
  })
}

// ── Polling fallback ───────────────────────────────────────────────
// The old dashboard polled every 3s as a fallback for SSE reliability.
// We do the same — if SSE is working, the poll is redundant but harmless.
function startPolling() {
  stopPolling()
  pollTimer = setInterval(() => {
    if (selectedSpace.value) {
      loadSpace(selectedSpace.value)
      loadTmuxStatus(selectedSpace.value)
    }
    loadSpaces()
  }, 5000)
}

function stopPolling() {
  if (pollTimer !== null) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

// ── Lifecycle ──────────────────────────────────────────────────────
onMounted(async () => {
  loading.value = true
  await loadSpaces()
  loading.value = false
  setupSSE()
  startPolling()

  if (selectedSpace.value) {
    // Route already has a space — load its data and connect SSE
    loadSpace(selectedSpace.value)
    loadTmuxStatus(selectedSpace.value)
    sse.connect(selectedSpace.value)
  } else if (spaces.value.length > 0) {
    // No space in URL — redirect to first space
    router.replace('/' + spaces.value[0]!.name)
  } else {
    // No spaces at all — connect to global SSE to catch new spaces
    sse.connect()
  }
})

onUnmounted(() => {
  sse.disconnect()
  stopPolling()
})
</script>

<template>
  <TooltipProvider>
    <!-- Router view (hidden) — routes are used purely for URL sync -->
    <router-view v-slot="{ Component }">
      <component :is="Component" v-show="false" />
    </router-view>
    <SidebarProvider>
      <AppSidebar
        :spaces="spaces"
        :current-space="currentSpace"
        :selected-space="selectedSpace"
        :selected-agent="selectedAgent"
        @select-space="handleSelectSpace"
        @select-agent="handleSelectAgent"
        @broadcast="handleBroadcastSpace"
      />
      <SidebarInset class="flex flex-col h-dvh">
        <!-- Header -->
        <header class="flex items-center gap-3 h-14 shrink-0 border-b px-4">
          <SidebarTrigger class="-ml-1" />
          <Separator orientation="vertical" class="h-5" />
          <nav aria-label="Breadcrumb" class="flex items-center gap-2 text-sm font-text">
            <span class="text-primary font-bold text-lg font-sans">Agent Boss</span>
            <template v-if="selectedSpace">
              <span class="text-muted-foreground">/</span>
              <button
                class="text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
                :class="{ 'text-foreground font-medium': !selectedAgent }"
                @click="router.push('/' + selectedSpace)"
              >
                {{ selectedSpace }}
              </button>
              <template v-if="selectedAgent">
                <span class="text-muted-foreground">/</span>
                <span class="text-foreground font-medium">{{ selectedAgent }}</span>
              </template>
            </template>
          </nav>
          <!-- SSE connection indicator + theme toggle -->
          <div class="ml-auto flex items-center gap-3">
            <span
              :class="[
                'inline-block size-2 rounded-full',
                sse.connected.value ? 'bg-green-500' : 'bg-muted-foreground',
              ]"
              :title="sse.connected.value ? 'Live connection active' : 'Disconnected — reconnecting...'"
              :aria-label="sse.connected.value ? 'Live connection active' : 'Disconnected, reconnecting'"
              role="status"
            />
            <span class="text-xs text-muted-foreground font-text hidden sm:inline">
              {{ sse.connected.value ? 'Live' : 'Reconnecting...' }}
            </span>
            <Button
              variant="ghost"
              size="icon-sm"
              :aria-label="theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'"
              :title="theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'"
              @click="toggleTheme"
            >
              <!-- Sun icon (shown in dark mode) -->
              <svg v-if="theme === 'dark'" xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/></svg>
              <!-- Moon icon (shown in light mode) -->
              <svg v-else xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/></svg>
            </Button>
          </div>
        </header>

        <!-- Error toast -->
        <div
          v-if="errorMessage"
          role="alert"
          class="mx-4 mt-2 rounded-md border border-destructive/50 bg-destructive/10 px-4 py-2 text-sm text-destructive flex items-center justify-between gap-2"
        >
          <span>{{ errorMessage }}</span>
          <button
            class="text-destructive hover:text-foreground text-xs font-medium shrink-0"
            aria-label="Dismiss error"
            @click="errorMessage = null"
          >
            Dismiss
          </button>
        </div>

        <!-- Screen reader announcements for live updates -->
        <div aria-live="polite" aria-atomic="true" class="sr-only">
          {{ statusAnnouncement }}
        </div>

        <!-- Main content -->
        <main class="flex-1 min-h-0 overflow-hidden" aria-label="Dashboard content">
          <!-- Loading state -->
          <div v-if="loading" class="flex flex-col items-center justify-center h-full text-muted-foreground font-text gap-3">
            <div class="h-8 w-8 animate-spin rounded-full border-2 border-muted-foreground border-t-primary" role="status">
              <span class="sr-only">Loading...</span>
            </div>
            <p class="text-sm">Loading spaces...</p>
          </div>

          <!-- Agent detail view -->
          <AgentDetail
            v-else-if="selectedAgentData && selectedAgent"
            :agent="selectedAgentData"
            :agent-name="selectedAgent"
            :space-name="selectedSpace"
            :tmux-status="selectedAgentTmux"
            @approve="handleApproveAgent"
            @reply="handleReplyAgent"
            @broadcast="handleBroadcastAgent"
            @delete="handleDeleteAgent()"
            @dismiss-question="handleDismissQuestion"
            @dismiss-blocker="handleDismissBlocker"
            @send-message="handleSendMessage"
          />

          <!-- Space overview -->
          <SpaceOverview
            v-else-if="currentSpace"
            :space="currentSpace"
            :tmux-status="tmuxStatus"
            @select-agent="handleSelectAgent"
            @broadcast="handleBroadcastSpace"
            @delete-agent="handleDeleteAgent"
            @broadcast-agent="handleBroadcastSingleAgent"
            @send-message-to-agent="handleSendMessageToAgent"
          />

          <!-- Empty state -->
          <div v-else class="flex flex-col items-center justify-center h-full text-muted-foreground font-text px-4 text-center">
            <div class="h-12 w-1 rounded-full bg-primary mb-4" aria-hidden="true" />
            <p class="text-lg font-sans font-semibold mb-1">Agent Boss</p>
            <p class="text-sm mb-4">Multi-agent coordination dashboard</p>
            <p v-if="spaces.length === 0" class="text-sm">
              No spaces found. Agents will create spaces automatically when they register.
            </p>
            <p v-else class="text-sm">
              Select a space from the sidebar to view agents.
            </p>
          </div>
        </main>

        <!-- Event Log panel -->
        <EventLog
          ref="eventLogRef"
          :space-name="selectedSpace"
        />
      </SidebarInset>
    </SidebarProvider>
  </TooltipProvider>
</template>
