export const MIN_SCALE = 0.05
export const MAX_SCALE = 40

export function applyLayerView(view, mapLayer, markerLayer) {
  const transform = `translate(${view.x}px, ${view.y}px) scale(${view.k})`
  if (mapLayer) mapLayer.style.transform = transform
  if (markerLayer) {
    markerLayer.style.transform = transform
    markerLayer.style.setProperty('--marker-inverse-scale', String(1 / view.k))
  }
}

export function clampScale(scale, minScale = MIN_SCALE, maxScale = MAX_SCALE) {
  return Math.min(maxScale, Math.max(minScale, scale))
}

export function midpoint(a, b) {
  return { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 }
}

export function pointDistance(a, b) {
  return Math.hypot(a.x - b.x, a.y - b.y)
}

export function panFrom(startView, startPoint, currentPoint) {
  return {
    ...startView,
    x: startView.x + currentPoint.x - startPoint.x,
    y: startView.y + currentPoint.y - startPoint.y,
  }
}

export function beginPinch(view, a, b) {
  const center = midpoint(a, b)
  const distance = pointDistance(a, b)
  return {
    view: { ...view },
    center,
    distance,
    anchor: {
      x: (center.x - view.x) / view.k,
      y: (center.y - view.y) / view.k,
    },
  }
}

export function pinchFrom(start, a, b, minScale = MIN_SCALE, maxScale = MAX_SCALE) {
  const center = midpoint(a, b)
  const distance = pointDistance(a, b)
  const factor = start.distance > 0 && distance > 0 ? distance / start.distance : 1
  const k = clampScale(start.view.k * factor, minScale, maxScale)
  return {
    k,
    x: center.x - start.anchor.x * k,
    y: center.y - start.anchor.y * k,
  }
}
