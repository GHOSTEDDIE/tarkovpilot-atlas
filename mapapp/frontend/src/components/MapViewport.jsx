import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
} from 'react'
import { Popover, Typography } from 'antd'
import { project, headingAngle } from '../lib/projection'
import {
  applyLayerView,
  beginPinch,
  clampScale,
  panFrom,
  pinchFrom,
  pointDistance,
} from '../lib/viewportGesture'

const { Text } = Typography

// Interactive map: inline SVG + pan/zoom (mouse, wheel, touch pinch) + markers.
//
// The view transform lives in a ref and is written straight to the two layer
// roots so panning does not trigger React renders or per-marker DOM writes.
// Markers counter-scale through one inherited CSS variable and stay the same
// visual size at every zoom level.
const MapViewport = forwardRef(function MapViewport(
  { map, svgText, floor, position, history, calibMode, calibPoints, onCalibTap, follow, onManualMove, onSvgSize, taskMarkers, featureMarkers, onOpenTask },
  ref,
) {
  const viewportRef = useRef(null)
  const transformRef = useRef(null)
  const markersRef = useRef(null)
  const holderRef = useRef(null)
  const view = useRef({ x: 0, y: 0, k: 1 })
  const pointers = useRef(new Map())
  const gesture = useRef(null)
  const suppressClickUntil = useRef(0)
  const [svgSize, setSvgSize] = useState(null)

  const applyView = useCallback(() => {
    applyLayerView(view.current, transformRef.current, markersRef.current)
  }, [])

  const fitView = useCallback(() => {
    const vp = viewportRef.current
    if (!vp || !svgSize) return
    const k = Math.min(vp.clientWidth / svgSize.w, vp.clientHeight / svgSize.h) * 0.96
    view.current = {
      k,
      x: (vp.clientWidth - svgSize.w * k) / 2,
      y: (vp.clientHeight - svgSize.h * k) / 2,
    }
    applyView()
  }, [svgSize, applyView])

  const zoomAt = useCallback(
    (cx, cy, factor) => {
      const vp = viewportRef.current
      if (!vp) return
      const r = vp.getBoundingClientRect()
      const sx = (cx - r.left - view.current.x) / view.current.k
      const sy = (cy - r.top - view.current.y) / view.current.k
      const k = clampScale(view.current.k * factor)
      view.current = { k, x: cx - r.left - sx * k, y: cy - r.top - sy * k }
      applyView()
    },
    [applyView],
  )

  const zoomCenter = useCallback(
    (factor) => {
      const vp = viewportRef.current
      if (!vp) return
      const r = vp.getBoundingClientRect()
      zoomAt(r.left + r.width / 2, r.top + r.height / 2, factor)
    },
    [zoomAt],
  )

  const centerOn = useCallback(
    (sx, sy) => {
      const vp = viewportRef.current
      if (!vp) return
      view.current.x = vp.clientWidth / 2 - sx * view.current.k
      view.current.y = vp.clientHeight / 2 - sy * view.current.k
      applyView()
    },
    [applyView],
  )

  const centerOnPlayer = useCallback(() => {
    if (!map || !position || !svgSize) return
    const [sx, sy] = project(map, position.world, svgSize)
    centerOn(sx, sy)
  }, [map, position, svgSize, centerOn])

  useImperativeHandle(ref, () => ({ fitView, zoomAt, zoomCenter, centerOn, centerOnPlayer }), [fitView, zoomAt, zoomCenter, centerOn, centerOnPlayer])

  // inject the SVG, measure the viewBox
  useEffect(() => {
    if (!holderRef.current || !svgText) return
    holderRef.current.innerHTML = svgText
    const svg = holderRef.current.querySelector('svg')
    if (!svg) return
    const vb = (svg.getAttribute('viewBox') || '').trim().split(/\s+/).map(Number)
    if (vb.length !== 4 || vb.some(isNaN)) return
    const size = { w: vb[2], h: vb[3] }
    svg.setAttribute('width', size.w)
    svg.setAttribute('height', size.h)
    svg.style.display = 'block'
    setSvgSize(size)
    onSvgSize?.(size)
  }, [svgText])

  // initial fit once the SVG is measured
  useEffect(() => {
    if (svgSize) fitView()
  }, [svgSize, fitView])

  // floor visibility: show only the selected floor group
  useEffect(() => {
    if (!holderRef.current || !map || !floor) return
    const ids = new Set(map.floors)
    for (const g of holderRef.current.querySelectorAll('g[id]')) {
      if (ids.has(g.id)) g.style.display = g.id === floor ? '' : 'none'
    }
  }, [map, floor, svgText])

  // follow the player
  useEffect(() => {
    if (follow) centerOnPlayer()
  }, [follow, centerOnPlayer])

  // position markers after every render (they are recreated by React)
  useEffect(() => {
    applyView()
  })

  // wheel zoom needs a non-passive listener
  useEffect(() => {
    const vp = viewportRef.current
    if (!vp) return
    const onWheel = (e) => {
      e.preventDefault()
      onManualMove?.()
      zoomAt(e.clientX, e.clientY, e.deltaY < 0 ? 1.15 : 1 / 1.15)
    }
    vp.addEventListener('wheel', onWheel, { passive: false })
    return () => vp.removeEventListener('wheel', onWheel)
  }, [zoomAt, onManualMove])

  const localPoint = (e) => {
    const r = viewportRef.current.getBoundingClientRect()
    return { x: e.clientX - r.left, y: e.clientY - r.top }
  }

  const capturePointers = () => {
    const vp = viewportRef.current
    if (!vp) return
    for (const pointerId of pointers.current.keys()) {
      if (!vp.hasPointerCapture(pointerId)) vp.setPointerCapture(pointerId)
    }
  }

  const onPointerDown = (e) => {
    if (e.pointerType === 'mouse' && e.button !== 0) return
    if (!pointers.current.has(e.pointerId) && pointers.current.size >= 2) return
    const point = localPoint(e)
    pointers.current.set(e.pointerId, point)
    if (pointers.current.size === 2) {
      const [a, b] = [...pointers.current.values()]
      gesture.current = { kind: 'pinch', start: beginPinch(view.current, a, b), moved: true }
      onManualMove?.()
      capturePointers()
      e.preventDefault()
    } else if (pointers.current.size === 1) {
      gesture.current = {
        kind: 'pan',
        pointerId: e.pointerId,
        startPoint: point,
        startView: { ...view.current },
        moved: false,
        pointerType: e.pointerType,
      }
    }
  }

  const onPointerMove = (e) => {
    if (!pointers.current.has(e.pointerId)) return
    if (e.pointerType === 'mouse' && e.buttons === 0) {
      finishPointer(e.pointerId)
      return
    }
    const point = localPoint(e)
    pointers.current.set(e.pointerId, point)
    if (pointers.current.size === 1 && gesture.current?.kind === 'pan') {
      const threshold = gesture.current.pointerType === 'touch' ? 6 : 3
      if (!gesture.current.moved && pointDistance(gesture.current.startPoint, point) >= threshold) {
        gesture.current.moved = true
        onManualMove?.()
        capturePointers()
      }
      if (!gesture.current.moved) return
      view.current = panFrom(gesture.current.startView, gesture.current.startPoint, point)
      applyView()
      e.preventDefault()
    } else if (pointers.current.size === 2 && gesture.current?.kind === 'pinch') {
      const [a, b] = [...pointers.current.values()]
      view.current = pinchFrom(gesture.current.start, a, b)
      applyView()
      e.preventDefault()
    }
  }

  const finishPointer = (pointerId) => {
    const existed = pointers.current.delete(pointerId)
    if (!existed) return false
    if (pointers.current.size === 1) {
      const [remainingId, point] = [...pointers.current.entries()][0]
      gesture.current = {
        kind: 'pan',
        pointerId: remainingId,
        startPoint: point,
        startView: { ...view.current },
        moved: true,
        pointerType: 'touch',
      }
    } else if (pointers.current.size === 0) {
      gesture.current = null
    }
    return true
  }

  const onPointerUp = (e) => {
    const currentGesture = gesture.current
    if (!finishPointer(e.pointerId)) return
    const moved = currentGesture?.moved || currentGesture?.kind === 'pinch'
    if (moved) suppressClickUntil.current = performance.now() + 400
    if (calibMode && !moved && pointers.current.size === 0 && viewportRef.current) {
      const r = viewportRef.current.getBoundingClientRect()
      const sx = (e.clientX - r.left - view.current.x) / view.current.k
      const sy = (e.clientY - r.top - view.current.y) / view.current.k
      onCalibTap?.([sx, sy])
    }
  }

  const onPointerCancel = (e) => {
    finishPointer(e.pointerId)
  }

  const onClickCapture = (e) => {
    if (performance.now() > suppressClickUntil.current) return
    e.preventDefault()
    e.stopPropagation()
  }

  const trail = useMemo(
    () => (history || []).filter((e) => e.mapId === map?.id).slice(-60),
    [history, map],
  )

  const heading =
    map && position?.rotation && svgSize
      ? headingAngle(map, position.world, position.rotation, svgSize)
      : null

  const playerPt = map && position && svgSize ? project(map, position.world, svgSize) : null

  return (
    <div
      ref={viewportRef}
      className="map-viewport relative flex-1 overflow-hidden"
      aria-label="游戏地图，可单指拖动、双指缩放"
      onPointerDownCapture={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerCancel}
      onLostPointerCapture={onPointerCancel}
      onClickCapture={onClickCapture}
    >
      <div ref={transformRef} className="map-transform">
        <div
          className="relative"
          style={svgSize ? { width: svgSize.w, height: svgSize.h } : undefined}
        >
          <div ref={holderRef} />
        </div>
      </div>
      <div ref={markersRef} className="markers-layer">
        {map && svgSize && (
          <>
            {trail.map((ev, i) => {
              const [sx, sy] = project(map, ev.world, svgSize)
              return <div key={i} className="marker trail-dot" data-sx={sx} data-sy={sy} style={{ left: sx, top: sy }} />
            })}
            {calibPoints.map((pt, i) => (
              <div key={`c${i}`} className="marker calib-marker" data-sx={pt.s[0]} data-sy={pt.s[1]} style={{ left: pt.s[0], top: pt.s[1] }}>
                {i + 1}
              </div>
            ))}
            {(featureMarkers || []).map((fm) => (
              <Popover
                key={fm.key}
                trigger="click"
                title={fm.feature.nameZh || fm.feature.name}
                content={
                  <div className="max-w-72 text-xs leading-5">
                    <div>{fm.feature.kind === 'transit' ? '地图转场' : fm.feature.faction === 'pmc' ? 'PMC 撤离点' : fm.feature.faction === 'scav' ? 'Scav 撤离点' : '共享撤离点'}</div>
                    {fm.feature.destinationMapId && <div>前往：{fm.feature.destinationMapId}</div>}
                    {fm.feature.conditions && <div>条件：{fm.feature.conditions}</div>}
                    {(fm.feature.switchIds || []).length > 0 && <div>关联开关：{fm.feature.switchIds.join('、')}</div>}
                    {fm.feature.floorPending && <div className="text-amber-400">楼层待确认</div>}
                    <Text type="secondary">坐标：{fm.feature.position.x.toFixed(1)}, {fm.feature.position.y.toFixed(1)}, {fm.feature.position.z.toFixed(1)}</Text>
                  </div>
                }
              >
                <div
                  className={`marker feature-marker ${fm.feature.kind} ${fm.feature.faction || ''}`}
                  data-sx={fm.px}
                  data-sy={fm.py}
                  style={{ left: fm.px, top: fm.py }}
                  onPointerDown={(e) => e.stopPropagation()}
                >
                  <div className="feature-symbol" />
                </div>
              </Popover>
            ))}
            {(taskMarkers || []).map((marker) => (
              <div
                key={marker.key}
                className={`marker quest-marker${marker.completed ? ' completed' : ''}`}
                data-sx={marker.px}
                data-sy={marker.py}
                style={{ left: marker.px, top: marker.py }}
                title={marker.task.nameZh || marker.task.name}
                onPointerDown={(event) => event.stopPropagation()}
                onClick={(event) => {
                  event.stopPropagation()
                  onOpenTask?.(marker)
                }}
              >
                <div className="gem" />
              </div>
            ))}
            {playerPt && (
              <div className="marker player-marker" data-sx={playerPt[0]} data-sy={playerPt[1]} style={{ left: playerPt[0], top: playerPt[1] }}>
                {heading !== null && heading !== undefined && (
                  <div className="arrow" style={{ transform: `rotate(${(heading * 180) / Math.PI + 90}deg)` }} />
                )}
                <div className="dot" />
              </div>
            )}
          </>
        )}
      </div>
	  {map?.attribution && (
		<a
		  className="map-attribution"
		  href={map.attributionUrl}
		  target="_blank"
		  rel="noreferrer"
		  onPointerDown={(event) => event.stopPropagation()}
		>
		  地图：{map.attribution}
		</a>
	  )}
    </div>
  )
})

export default MapViewport
