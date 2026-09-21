import test from 'node:test'
import assert from 'node:assert/strict'
import { applyLayerView, beginPinch, panFrom, pinchFrom } from './viewportGesture.js'

function close(actual, expected) {
  assert.ok(Math.abs(actual - expected) < 1e-9, `${actual} != ${expected}`)
}

test('pan keeps the initial scale and follows the pointer delta', () => {
  assert.deepEqual(
    panFrom({ x: 10, y: 20, k: 2 }, { x: 40, y: 50 }, { x: 55, y: 42 }),
    { x: 25, y: 12, k: 2 },
  )
})

test('pinch keeps the map point under the moving finger center', () => {
  const start = beginPinch(
    { x: 20, y: 30, k: 2 },
    { x: 80, y: 100 },
    { x: 120, y: 100 },
  )
  const next = pinchFrom(start, { x: 100, y: 130 }, { x: 180, y: 130 })

  close(next.k, 4)
  close(next.x + start.anchor.x * next.k, 140)
  close(next.y + start.anchor.y * next.k, 130)
})

test('pinch respects zoom limits without moving its current center', () => {
  const start = beginPinch(
    { x: 0, y: 0, k: 1 },
    { x: 40, y: 50 },
    { x: 60, y: 50 },
  )
  const next = pinchFrom(start, { x: -50, y: 50 }, { x: 150, y: 50 }, 0.5, 3)

  close(next.k, 3)
  close(next.x + start.anchor.x * next.k, 50)
  close(next.y + start.anchor.y * next.k, 50)
})

test('panning updates map and marker layers without touching every marker', () => {
  const writes = []
  const mapLayer = { style: { set transform(value) { writes.push(['map', value]) } } }
  const markerLayer = {
    children: [{ style: { set transform(_) { throw new Error('marker rewritten') } } }],
    style: {
      set transform(value) { writes.push(['markers', value]) },
      setProperty(name, value) { writes.push([name, value]) },
    },
  }

  applyLayerView({ x: 12, y: -8, k: 2 }, mapLayer, markerLayer)

  assert.deepEqual(writes, [
    ['map', 'translate(12px, -8px) scale(2)'],
    ['markers', 'translate(12px, -8px) scale(2)'],
    ['--marker-inverse-scale', '0.5'],
  ])
})
