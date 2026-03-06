<script setup lang="ts">
import type { AgentStatus } from '@/types'
import { STATUS_DISPLAY } from '@/types'
import { Badge } from '@/components/ui/badge'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { computed } from 'vue'

const props = defineProps<{
  status: AgentStatus
}>()

const display = computed(() => STATUS_DISPLAY[props.status] ?? { label: props.status, tooltip: '' })

const config = computed(() => {
  switch (props.status) {
    case 'active':
      return { dotClass: 'bg-green-500', badgeClass: 'bg-green-500/15 text-green-400 border-green-500/30' }
    case 'blocked':
      return { dotClass: 'bg-primary', badgeClass: 'bg-primary/15 text-primary border-primary/30' }
    case 'done':
      return { dotClass: 'bg-teal-500', badgeClass: 'bg-teal-500/15 text-teal-400 border-teal-500/30' }
    case 'idle':
      return { dotClass: 'bg-muted-foreground', badgeClass: 'bg-muted text-muted-foreground border-border' }
    case 'error':
      return { dotClass: 'bg-destructive', badgeClass: 'bg-destructive/15 text-destructive border-destructive/30' }
    default:
      return { dotClass: 'bg-muted-foreground', badgeClass: 'bg-muted text-muted-foreground border-border' }
  }
})
</script>

<template>
  <Tooltip>
    <TooltipTrigger as-child>
      <Badge variant="outline" :class="config.badgeClass" role="status" :aria-label="`Task Status: ${display.label}`">
        <span :class="['inline-block size-2 rounded-full', config.dotClass]" aria-hidden="true" />
        {{ display.label }}
      </Badge>
    </TooltipTrigger>
    <TooltipContent>
      Task Status: {{ display.label }} — {{ display.tooltip }}
    </TooltipContent>
  </Tooltip>
</template>
