<script setup lang="ts">
import type { KnowledgeSpace, TmuxAgentStatus } from '@/types'
import { ref, computed, nextTick } from 'vue'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Radio, Bell, Trash2, MessageSquare, SendHorizontal, HelpCircle, AlertTriangle, MessageSquareReply } from 'lucide-vue-next'
import StatusBadge from './StatusBadge.vue'
import InterruptTracker from './InterruptTracker.vue'

const props = defineProps<{
  space: KnowledgeSpace
  tmuxStatus: Record<string, TmuxAgentStatus> | null
}>()

const emit = defineEmits<{
  'select-agent': [name: string]
  broadcast: []
  'delete-agent': [name: string]
  'broadcast-agent': [name: string]
  'send-message-to-agent': [agentName: string, text: string]
}>()

const deleteDialogOpen = ref(false)
const deleteDialogAgent = ref<string | null>(null)
const messageAgent = ref<string | null>(null)
const messageText = ref('')
const messageInputRef = ref<InstanceType<typeof Input> | null>(null)

function openDeleteDialog(name: string) {
  deleteDialogAgent.value = name
  deleteDialogOpen.value = true
}

function confirmDeleteAgent() {
  if (deleteDialogAgent.value) {
    emit('delete-agent', deleteDialogAgent.value)
  }
  deleteDialogOpen.value = false
  deleteDialogAgent.value = null
}

function openMessageInput(name: string) {
  messageAgent.value = name
  messageText.value = ''
  nextTick(() => {
    const el = messageInputRef.value?.$el as HTMLInputElement | undefined
    el?.focus()
  })
}

function sendQuickMessage(name: string) {
  const text = messageText.value.trim()
  if (!text) return
  emit('send-message-to-agent', name, text)
  messageAgent.value = null
  messageText.value = ''
}

function relativeTime(dateStr: string): string {
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

function formatFullDate(dateStr: string): string {
  return new Date(dateStr).toLocaleString()
}

function handleCardKeydown(e: KeyboardEvent, name: string) {
  // Don't intercept keys when the user is typing in an input/textarea/button
  const tag = (e.target as HTMLElement)?.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'BUTTON') return
  if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault()
    emit('select-agent', name)
  }
}

const sortedAgents = computed(() => {
  return Object.entries(props.space.agents).sort(([, a], [, b]) => {
    return new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime()
  })
})

const agentCount = computed(() => Object.keys(props.space.agents).length)

const inboxRef = ref<InstanceType<typeof InterruptTracker> | null>(null)

// Compute attention count directly from agent data (doesn't depend on InterruptTracker mounting)
const attentionCount = computed(() => {
  let count = 0
  for (const agent of Object.values(props.space.agents)) {
    count += (agent.questions?.length ?? 0) + (agent.blockers?.length ?? 0)
  }
  return count
})

// Use InterruptTracker's count if available (more accurate, includes interrupt types), otherwise fall back
const inboxPending = computed(() => inboxRef.value?.pendingCount ?? attentionCount.value)
</script>

<template>
  <ScrollArea class="h-full">
    <div class="p-6 space-y-6 max-w-5xl">
      <!-- Header -->
      <div class="flex items-center justify-between">
        <div>
          <h1 class="text-2xl font-semibold tracking-tight">{{ space.name }}</h1>
          <p class="text-sm text-muted-foreground font-text">
            {{ agentCount }} agent{{ agentCount !== 1 ? 's' : '' }}
          </p>
        </div>
        <Tooltip>
          <TooltipTrigger as-child>
            <Button variant="outline" size="sm" @click="emit('broadcast')">
              <Radio class="size-4" /> Nudge All
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            Nudge all agents with the latest space state
          </TooltipContent>
        </Tooltip>
      </div>

      <!-- Tabs: Agents / Inbox -->
      <Tabs default-value="agents">
        <TabsList>
          <TabsTrigger value="agents">Agents</TabsTrigger>
          <TabsTrigger value="inbox" class="gap-1.5">
            Inbox
            <Badge
              v-if="inboxPending > 0"
              variant="destructive"
              class="h-5 min-w-5 px-1 text-[10px] font-semibold tabular-nums"
            >
              {{ inboxPending }}
            </Badge>
          </TabsTrigger>
        </TabsList>

        <TabsContent value="agents">
          <!-- Agent Grid -->
          <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3" role="list" aria-label="Agents in this space">
            <Card
              v-for="[name, agent] in sortedAgents"
              :key="name"
              role="listitem"
              tabindex="0"
              class="group cursor-pointer transition-colors hover:bg-accent/50 focus-visible:outline-2 focus-visible:outline-ring focus-visible:outline-offset-2 relative overflow-hidden"
              :class="{
                'border-l-4 border-l-amber-500': agent.questions?.length && !agent.blockers?.length,
                'border-l-4 border-l-red-500': agent.blockers?.length,
              }"
              :aria-label="`Agent ${name}, status: ${agent.status}${agent.summary ? ', ' + agent.summary : ''}`"
              @click="emit('select-agent', name)"
              @keydown="handleCardKeydown($event, name)"
            >
              <!-- Attention banner for questions/blockers -->
              <div
                v-if="agent.questions?.length || agent.blockers?.length"
                class="px-4 pt-3 pb-0 space-y-1.5"
                @click.stop
              >
                <!-- Blocker banner -->
                <div
                  v-if="agent.blockers?.length"
                  class="flex items-start gap-2 rounded-md bg-red-500/10 border border-red-500/30 px-3 py-2"
                >
                  <AlertTriangle class="size-4 text-red-500 shrink-0 mt-0.5" />
                  <div class="flex-1 min-w-0">
                    <p class="text-xs font-semibold text-red-600 dark:text-red-400">
                      {{ agent.blockers.length }} Blocker{{ agent.blockers.length !== 1 ? 's' : '' }}
                    </p>
                    <p class="text-xs text-red-600/80 dark:text-red-400/80 font-text line-clamp-1 mt-0.5">
                      {{ agent.blockers[0] }}
                    </p>
                  </div>
                  <Button
                    variant="outline"
                    size="sm"
                    class="h-6 px-2 text-[10px] border-red-500/30 text-red-600 dark:text-red-400 hover:bg-red-500/10 shrink-0"
                    @click.stop="emit('select-agent', name)"
                  >
                    <MessageSquareReply class="size-3" /> Reply
                  </Button>
                </div>
                <!-- Question banner -->
                <div
                  v-if="agent.questions?.length"
                  class="flex items-start gap-2 rounded-md bg-amber-500/10 border border-amber-500/30 px-3 py-2"
                >
                  <HelpCircle class="size-4 text-amber-500 shrink-0 mt-0.5" />
                  <div class="flex-1 min-w-0">
                    <p class="text-xs font-semibold text-amber-600 dark:text-amber-400">
                      {{ agent.questions.length }} Question{{ agent.questions.length !== 1 ? 's' : '' }}
                    </p>
                    <p class="text-xs text-amber-600/80 dark:text-amber-400/80 font-text line-clamp-1 mt-0.5">
                      {{ agent.questions[0] }}
                    </p>
                  </div>
                  <Button
                    variant="outline"
                    size="sm"
                    class="h-6 px-2 text-[10px] border-amber-500/30 text-amber-600 dark:text-amber-400 hover:bg-amber-500/10 shrink-0"
                    @click.stop="emit('select-agent', name)"
                  >
                    <MessageSquareReply class="size-3" /> Reply
                  </Button>
                </div>
              </div>

              <CardHeader class="pb-2">
                <div class="flex items-center justify-between gap-2">
                  <CardTitle class="text-base truncate">{{ name }}</CardTitle>
                  <div class="flex items-center gap-1.5">
                    <StatusBadge :status="agent.status" />
                    <Tooltip v-if="tmuxStatus?.[name]?.needs_approval">
                      <TooltipTrigger as-child>
                        <Badge variant="outline" class="border-primary/50 text-primary text-[10px] h-5 px-1.5">
                          Approval
                        </Badge>
                      </TooltipTrigger>
                      <TooltipContent>
                        Agent is waiting for tool approval
                      </TooltipContent>
                    </Tooltip>
                  </div>
                </div>
              </CardHeader>
              <CardContent class="space-y-2">
                <p class="text-sm font-text text-muted-foreground line-clamp-2">
                  {{ agent.summary || 'No summary available' }}
                </p>
                <div class="flex items-center gap-3 text-xs text-muted-foreground font-text">
                  <span v-if="agent.phase" class="truncate" :title="`Current phase: ${agent.phase}`">{{ agent.phase }}</span>
                  <Tooltip v-if="agent.branch">
                    <TooltipTrigger as-child>
                      <span class="font-mono bg-muted px-1 py-0.5 rounded truncate max-w-[120px] cursor-default">
                        {{ agent.branch }}
                      </span>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>Branch: {{ agent.branch }}</p>
                      <p v-if="agent.repo_url">Repo: {{ agent.repo_url }}</p>
                    </TooltipContent>
                  </Tooltip>
                </div>
                <div class="flex items-center justify-between text-xs text-muted-foreground font-text">
                  <Tooltip>
                    <TooltipTrigger as-child>
                      <span class="cursor-default" :aria-label="`Updated ${relativeTime(agent.updated_at)}, at ${formatFullDate(agent.updated_at)}`">
                        {{ relativeTime(agent.updated_at) }}
                      </span>
                    </TooltipTrigger>
                    <TooltipContent>
                      {{ formatFullDate(agent.updated_at) }}
                    </TooltipContent>
                  </Tooltip>
                </div>
              </CardContent>

              <!-- Inline quick-message input -->
              <div
                v-if="messageAgent === name"
                class="px-4 pb-3"
                @click.stop
              >
                <form class="flex gap-2" @submit.prevent="sendQuickMessage(name)">
                  <Input
                    ref="messageInputRef"
                    v-model="messageText"
                    placeholder="Quick message..."
                    class="h-8 text-sm"
                    @keydown.escape="messageAgent = null"
                  />
                  <Button type="submit" size="sm" class="h-8 px-3 shrink-0" :disabled="!messageText.trim()">
                    <SendHorizontal class="size-3.5" /> Send
                  </Button>
                </form>
              </div>

              <!-- Card footer with action buttons -->
              <CardFooter class="pt-0 pb-3 px-4 gap-2" @click.stop>
                <Button variant="outline" size="sm" class="h-7 text-xs" @click.stop="emit('broadcast-agent', name)">
                  <Bell class="size-3.5" /> Nudge
                </Button>
                <Button variant="outline" size="sm" class="h-7 text-xs" @click.stop="openMessageInput(name)">
                  <MessageSquare class="size-3.5" /> Message
                </Button>
                <Button variant="ghost" size="sm" class="h-7 text-xs text-destructive hover:text-destructive ml-auto" @click.stop="openDeleteDialog(name)">
                  <Trash2 class="size-3.5" /> Delete
                </Button>
              </CardFooter>
            </Card>
          </div>

          <!-- Empty state -->
          <div v-if="agentCount === 0" class="flex flex-col items-center justify-center py-16 text-muted-foreground font-text text-center">
            <p class="text-lg">No agents in this space yet</p>
            <p class="text-sm mt-1">Agents will appear here when they register via the API</p>
          </div>
        </TabsContent>

        <TabsContent value="inbox">
          <InterruptTracker ref="inboxRef" :space-name="space.name" />
        </TabsContent>
      </Tabs>
      <!-- Delete agent AlertDialog -->
      <AlertDialog v-model:open="deleteDialogOpen">
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete agent?</AlertDialogTitle>
            <AlertDialogDescription>
              This will permanently remove <span class="font-semibold text-foreground">{{ deleteDialogAgent }}</span>. This cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction class="bg-destructive text-destructive-foreground hover:bg-destructive/90" @click="confirmDeleteAgent()">
              <Trash2 class="size-4" /> Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  </ScrollArea>
</template>
