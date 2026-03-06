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
  ['#ee0000', '#ff6b6b'], // red
  ['#f0ab00', '#ffd666'], // yellow
  ['#009596', '#48d1cc'], // teal
  ['#5e40be', '#9b8ec4'], // purple
  ['#ec7a08', '#f5a623'], // orange
  ['#4394e5', '#7ec8e3'], // blue
  ['#3e8635', '#73c06b'], // green
  ['#a30000', '#e74c3c'], // dark red
  ['#8b5cf6', '#c4b5fd'], // violet
  ['#06b6d4', '#67e8f9'], // cyan
]

// Produce a good 32-bit hash from a string
function hashCode(str: string): number {
  let h = 0x811c9dc5 // FNV offset basis
  for (let i = 0; i < str.length; i++) {
    h ^= str.charCodeAt(i)
    h = Math.imul(h, 0x01000193) // FNV prime
  }
  return h >>> 0 // unsigned
}

// Get multiple independent hash values from one seed
function multiHash(name: string, count: number): number[] {
  const hashes: number[] = []
  for (let i = 0; i < count; i++) {
    hashes.push(hashCode(name + String.fromCharCode(65 + i)))
  }
  return hashes
}

const avatar = computed(() => {
  const h = multiHash(props.name, 8)
  const s = props.size

  // Pick 3 distinct color pairs
  const c1 = PALETTE[h[0]! % PALETTE.length]!
  const c2 = PALETTE[(h[1]! % (PALETTE.length - 1) + 1 + (h[0]! % PALETTE.length)) % PALETTE.length]!
  const c3 = PALETTE[(h[2]! % (PALETTE.length - 2) + 2 + (h[0]! % PALETTE.length)) % PALETTE.length]!

  // Background gradient angle
  const bgAngle = (h[3]! % 360)

  // Shape parameters derived from hash
  const shapes: { type: string; cx: number; cy: number; r: number; fill: string; rotation: number }[] = []

  // Shape 1: Large background shape
  const s1Type = h[4]! % 3 // 0=circle, 1=square, 2=diamond
  shapes.push({
    type: ['circle', 'square', 'diamond'][s1Type]!,
    cx: s * 0.5,
    cy: s * 0.5,
    r: s * (0.38 + (h[4]! % 10) / 100),
    fill: c1[0]!,
    rotation: (h[5]! % 45),
  })

  // Shape 2: Medium accent shape, offset
  const s2Type = (h[5]! + 1) % 3
  const s2Angle = (h[5]! % 628) / 100 // 0 to 2π
  const s2Dist = s * (0.08 + (h[6]! % 15) / 100)
  shapes.push({
    type: ['circle', 'square', 'diamond'][s2Type]!,
    cx: s * 0.5 + Math.cos(s2Angle) * s2Dist,
    cy: s * 0.5 + Math.sin(s2Angle) * s2Dist,
    r: s * (0.22 + (h[6]! % 8) / 100),
    fill: c2[0]!,
    rotation: (h[6]! % 90),
  })

  // Shape 3: Small detail shape
  const s3Type = (h[7]! + 2) % 3
  const s3Angle = ((h[7]! + 314) % 628) / 100
  const s3Dist = s * (0.12 + (h[7]! % 12) / 100)
  shapes.push({
    type: ['circle', 'square', 'diamond'][s3Type]!,
    cx: s * 0.5 + Math.cos(s3Angle) * s3Dist,
    cy: s * 0.5 + Math.sin(s3Angle) * s3Dist,
    r: s * (0.12 + (h[7]! % 6) / 100),
    fill: c3[1]!,
    rotation: (h[7]! % 180),
  })

  return {
    bgColor1: c1[1]!,
    bgColor2: c2[1]!,
    bgAngle,
    shapes,
  }
})

const gradientId = computed(() => `ag-${hashCode(props.name)}`)
const radius = computed(() => props.size * 0.15)
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
    <defs>
      <linearGradient :id="gradientId" :gradientTransform="`rotate(${avatar.bgAngle})`">
        <stop offset="0%" :stop-color="avatar.bgColor1" stop-opacity="0.25" />
        <stop offset="100%" :stop-color="avatar.bgColor2" stop-opacity="0.25" />
      </linearGradient>
      <clipPath :id="`clip-${gradientId}`">
        <rect x="0" y="0" :width="size" :height="size" :rx="radius" :ry="radius" />
      </clipPath>
    </defs>

    <g :clip-path="`url(#clip-${gradientId})`">
      <!-- Background gradient -->
      <rect x="0" y="0" :width="size" :height="size" :fill="`url(#${gradientId})`" />

      <!-- Geometric shapes -->
      <template v-for="(shape, i) in avatar.shapes" :key="i">
        <circle
          v-if="shape.type === 'circle'"
          :cx="shape.cx"
          :cy="shape.cy"
          :r="shape.r"
          :fill="shape.fill"
          opacity="0.8"
        />
        <rect
          v-else-if="shape.type === 'square'"
          :x="shape.cx - shape.r"
          :y="shape.cy - shape.r"
          :width="shape.r * 2"
          :height="shape.r * 2"
          :fill="shape.fill"
          opacity="0.8"
          :transform="`rotate(${shape.rotation} ${shape.cx} ${shape.cy})`"
        />
        <rect
          v-else-if="shape.type === 'diamond'"
          :x="shape.cx - shape.r"
          :y="shape.cy - shape.r"
          :width="shape.r * 2"
          :height="shape.r * 2"
          :fill="shape.fill"
          opacity="0.8"
          :transform="`rotate(${45 + shape.rotation} ${shape.cx} ${shape.cy})`"
        />
      </template>
    </g>
  </svg>
</template>
