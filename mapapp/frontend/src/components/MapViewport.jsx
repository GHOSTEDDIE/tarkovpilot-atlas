import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
} from 'react'
import { Button, Popover, Space, Typography } from 'antd'
import { project, headingAngle } from '../lib/projection'

const { Text } = Typography

// Interactive map: inline SVG + pan/zoom (mouse, wheel, touch pinch) + markers.
//
// The view transform lives in a ref and is written straight to the DOM so
// panning stays at 60fps without React re-renders. Markers are NOT inside the
// scaled layer: each carries its map coordinates in data-sx/data-sy and is
// positioned in screen pixels with translate() on every view change, so
// markers keep exact pixel sizes and never distort at any zoom level.
const MapViewport = forwardRef(function MapViewport(
  { map, svgText, floor, position, history, calibMode, calibPoints, onCalibTap, follow, onSvgSize, questMarkers, onQuestStatus },
  ref,
) {
  const viewportRef = useRef(null)
  const transformRef = useRef(null)
  const markersRef = useRef(null)
  const holderRef = useRef(null)
  const view = useRef({ x: 0, y: 0, k: 1 })
  const pointers = useRef(new Map())
  const pinchDist = useRef(0)
  const dragged = useRef(false)
  const [svgSize, setSvgSize] = useState(null)

  const applyView = useCallback(() => {
    const { x, y, k } = view.current
    if (transformRef.current) {
      transformRef.current.style.transform = `translate(${x}px, ${y}px) scale(${k})`
    }
    if (markersRef.current) {
      for (const el of markersRef.current.children) {
        const sx = +el.dataset.sx
        const sy = +el.dataset.sy
        if (Number.isNaN(sx) || Number.isNaN(sy)) continue
        el.style.transform = `translate(${x + sx * k}px, ${y + sy * k}px)`
      }
    }
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
      const k = Math.min(40, Math.max(0.05, view.current.k * factor))
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
      zoomAt(e.clientX, e.clientY, e.deltaY < 0 ? 1.15 : 1 / 1.15)
    }
    vp.addEventListener('wheel', onWheel, { passive: false })
    return () => vp.removeEventListener('wheel', onWheel)
  }, [zoomAt])

  const onPointerDown = (e) => {
    viewportRef.current.setPointerCapture(e.pointerId)
    pointers.current.set(e.pointerId, [e.clientX, e.clientY])
    dragged.current = false
    if (pointers.current.size === 2) {
      const [a, b] = [...pointers.current.values()]
      pinchDist.current = Math.hypot(a[0] - b[0], a[1] - b[1])
    }
  }

  const onPointerMove = (e) => {
    if (!pointers.current.has(e.pointerId)) return
    const prev = pointers.current.get(e.pointerId)
    pointers.current.set(e.pointerId, [e.clientX, e.clientY])
    if (pointers.current.size === 1) {
      const dx = e.clientX - prev[0]
      const dy = e.clientY - prev[1]
      if (Math.abs(dx) + Math.abs(dy) > 2) dragged.current = true
      view.current.x += dx
      view.current.y += dy
      applyView()
    } else if (pointers.current.size === 2) {
      const [a, b] = [...pointers.current.values()]
      const d = Math.hypot(a[0] - b[0], a[1] - b[1])
      if (pinchDist.current > 0 && d > 0) {
        zoomAt((a[0] + b[0]) / 2, (a[1] + b[1]) / 2, d / pinchDist.current)
      }
      pinchDist.current = d
      dragged.current = true
    }
  }

  const onPointerUp = (e) => {
    pointers.current.delete(e.pointerId)
    if (calibMode && !dragged.current && pointers.current.size === 0 && viewportRef.current) {
      const r = viewportRef.current.getBoundingClientRect()
      const sx = (e.clientX - r.left - view.current.x) / view.current.k
      const sy = (e.clientY - r.top - view.current.y) / view.current.k
      onCalibTap?.([sx, sy])
    }
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
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={(e) => pointers.current.delete(e.pointerId)}
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
              return <div key={i} className="marker trail-dot" data-sx={sx} data-sy={sy} />
            })}
            {calibPoints.map((pt, i) => (
              <div key={`c${i}`} className="marker calib-marker" data-sx={pt.s[0]} data-sy={pt.s[1]}>
                {i + 1}
              </div>
            ))}
            {(questMarkers || []).map((qm) => (
              <Popover
                key={qm.key}
                trigger="click"
                title={
                  <>
                    {qm.quest.titleZh || qm.quest.title}
                    {(qm.quest.giverZh || qm.quest.giver) && (
                      <Text type="secondary" className="text-xs"> — {qm.quest.giverZh || qm.quest.giver}</Text>
                    )}
                  </>
                }
                content={
                  <div className="max-w-60">
                    <div className="text-xs mb-2">{qm.objective.descZh || qm.objective.desc}</div>
                    <Space>
                      <Button
                        size="small"
                        type={qm.completed ? 'default' : 'primary'}
                        onClick={() => onQuestStatus?.(qm.quest.id, qm.completed ? '' : 'completed')}
                      >
                        {qm.completed ? '取消完成' : '标记完成'}
                      </Button>
                    </Space>
                  </div>
                }
              >
                <div
                  className={`marker quest-marker${qm.completed ? ' completed' : ''}`}
                  data-sx={qm.px}
                  data-sy={qm.py}
                  onPointerDown={(e) => e.stopPropagation()}
                >
                  <div className="gem" />
                </div>
              </Popover>
            ))}
            {playerPt && (
              <div className="marker player-marker" data-sx={playerPt[0]} data-sy={playerPt[1]}>
                {heading !== null && heading !== undefined && (
                  <div className="arrow" style={{ transform: `rotate(${(heading * 180) / Math.PI + 90}deg)` }} />
                )}
                <div className="dot" />
              </div>
            )}
          </>
        )}
      </div>
    </div>
  )
})

export default MapViewport
