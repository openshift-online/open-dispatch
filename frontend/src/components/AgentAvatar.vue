<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    name: string
    size?: number
  }>(),
  { size: 32 },
)

const PALETTE = [
  '#ee0000', // red
  '#f0ab00', // yellow
  '#009596', // teal
  '#5e40be', // purple
  '#ec7a08', // orange
  '#4394e5', // blue
  '#3e8635', // green
]

function hashCode(str: string): number {
  let hash = 0
  for (let i = 0; i < str.length; i++) {
    hash = str.charCodeAt(i) + ((hash << 5) - hash)
    hash = hash & hash
  }
  return Math.abs(hash)
}

// Generate a symmetric 5x5 grid. Only compute 3 columns (0-2),
// mirror columns 3-4 from 1-0 to guarantee horizontal symmetry.
const grid = computed(() => {
  const h = hashCode(props.name)
  const cells: { x: number; y: number; fill: string }[] = []
  const bgColor = PALETTE[h % PALETTE.length]!

  for (let row = 0; row < 5; row++) {
    for (let col = 0; col < 3; col++) {
      // Use different bits of the hash for each cell
      const bit = (h >> (row * 3 + col)) & 1
      if (bit) {
        const color = PALETTE[(h + row * 3 + col) % PALETTE.length]!
        cells.push({ x: col, y: row, fill: color })
        // Mirror (except center column)
        if (col < 2) {
          cells.push({ x: 4 - col, y: row, fill: color })
        }
      }
    }
  }

  // Ensure at least some cells are filled — use a secondary hash if too sparse
  if (cells.length < 4) {
    const h2 = hashCode(props.name + '_salt')
    for (let row = 0; row < 5; row++) {
      for (let col = 0; col < 3; col++) {
        const bit = (h2 >> (row * 3 + col)) & 1
        if (bit) {
          const color = PALETTE[(h2 + row * 3 + col) % PALETTE.length]!
          cells.push({ x: col, y: row, fill: color })
          if (col < 2) {
            cells.push({ x: 4 - col, y: row, fill: color })
          }
        }
      }
    }
  }

  return { cells, bgColor }
})

const cellSize = computed(() => props.size / 5)
const radius = computed(() => props.size * 0.12) // ~12% corner radius
</script>

<template>
  <svg
    :width="size"
    :height="size"
    :viewBox="`0 0 ${size} ${size}`"
    :aria-label="`Avatar for ${name}`"
    role="img"
    class="inline-block shrink-0 rounded-md"
  >
    <!-- Background -->
    <rect
      x="0"
      y="0"
      :width="size"
      :height="size"
      :rx="radius"
      :ry="radius"
      :fill="grid.bgColor"
      opacity="0.15"
    />
    <!-- Grid cells -->
    <rect
      v-for="(cell, i) in grid.cells"
      :key="i"
      :x="cell.x * cellSize"
      :y="cell.y * cellSize"
      :width="cellSize"
      :height="cellSize"
      :fill="cell.fill"
      opacity="0.85"
    />
  </svg>
</template>
