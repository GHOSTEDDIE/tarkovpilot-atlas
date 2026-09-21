import assert from 'node:assert/strict'
import test from 'node:test'

import { rasterProject } from './projection.js'
import { buildFallbackSVG, buildRasterSVG } from './rasterMap.js'

const labyrinth = {
  projection: { horizontalAxes: ['x', 'z'], rotation: 270 },
  raster: {
    zoom: 2,
    tileSize: 256,
    transform: [2.115, 85.5, 2.115, 128],
    layers: [{ floor: 'Main_Level', tilePath: 'tiles/labyrinth/main/{z}/{x}-{y}.png' }],
  },
}

test('raster SVG contains the complete local tile grid', () => {
  const svg = buildRasterSVG(labyrinth, '/maps/', 'runtime-v1')
  assert.match(svg, /viewBox="0 0 1024 1024"/)
  assert.equal((svg.match(/<image /g) || []).length, 16)
  assert.match(svg, /\/maps\/tiles\/labyrinth\/main\/2\/3-3\.png\?v=runtime-v1/)
})

test('raster projection matches Tarkov.dev CRS.Simple transform', () => {
  const [x, y] = rasterProject(labyrinth, { x: 0, y: 0, z: 0 })
  assert.ok(Math.abs(x - 342) < 1e-9)
  assert.ok(Math.abs(y - 512) < 1e-9)
})

test('static fallback SVG uses registered image dimensions and local versioned URL', () => {
  const svg = buildFallbackSVG({
    fallbackImage: 'reference/lighthouse-2d.jpg',
    fallbackImageWidth: 2242,
    fallbackImageHeight: 3892,
  }, '/maps/', 'runtime-v2')
  assert.match(svg, /viewBox="0 0 2242 3892"/)
  assert.match(svg, /href="\/maps\/reference\/lighthouse-2d\.jpg\?v=runtime-v2"/)
})

test('static fallback SVG requires complete metadata', () => {
  assert.equal(buildFallbackSVG({ fallbackImage: 'map.jpg' }, '/maps/'), null)
})
