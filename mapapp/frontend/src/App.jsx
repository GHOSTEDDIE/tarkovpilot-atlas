import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Badge, Button, Drawer, Grid, Select, Spin, Tooltip, App as AntApp } from 'antd'
import {
  AimOutlined,
  CompressOutlined,
  MenuOutlined,
  MinusOutlined,
  PlusOutlined,
} from '@ant-design/icons'
import { useServerState, useSvgText, useQuests, postJSON } from './hooks/useServerState'
import { horizontalOf, project } from './lib/projection'
import MapViewport from './components/MapViewport'
import SidePanel from './components/SidePanel'

// floorForY mirrors the backend: pick the floor range containing worldY.
function floorForY(map, y) {
  for (const fr of map.floorRanges || []) {
    if (y >= fr.minY && y < fr.maxY) return fr.floor
  }
  return ''
}

export default function App() {
  const { message } = AntApp.useApp()
  const { state, connected } = useServerState()
  const screens = Grid.useBreakpoint()
  const isDesktop = !!screens.md

  const viewportCtl = useRef(null)
  const [curFloor, setCurFloor] = useState(null)
  const [follow, setFollow] = useState(true)
  const [calibMode, setCalibMode] = useState(false)
  const [calibPoints, setCalibPoints] = useState([])
  const [panelOpen, setPanelOpen] = useState(false)
  const [questFilter, setQuestFilter] = useState({ text: '', showCompleted: false })

  const quests = useQuests()

  const mapId = state?.mapId || null
  const map = state && mapId ? state.maps[mapId] : null
  const position = state?.position && state.position.mapId === mapId ? state.position : null

  // per-map calibration points persist in localStorage
  useEffect(() => {
    if (!mapId) return
    setCurFloor(null)
    setCalibMode(false)
    setCalibPoints(JSON.parse(localStorage.getItem('calib:' + mapId) || '[]'))
  }, [mapId])

  // default floor once the map is known
  useEffect(() => {
    if (map && (!curFloor || !map.floors.includes(curFloor))) {
      setCurFloor(map.defaultFloor)
    }
  }, [map, curFloor])

  // auto floor from the position's worldY
  useEffect(() => {
    if (map && position?.floor && map.floors.includes(position.floor)) {
      setCurFloor(position.floor)
    }
  }, [map, position])

  const svgUrl = map ? state.svgBaseUrl + map.svgFile : null
  const { text: svgText, error: svgError } = useSvgText(svgUrl || 'about:blank')

  const [svgSize, setSvgSize] = useState(null) // measured inside MapViewport via callback below

  const questStatus = state?.questStatus || {}

  const onQuestStatus = useCallback(async (questId, status) => {
    await postJSON(`/api/quests/${questId}/status`, { status })
  }, [])

  // quest objective markers for the current map/floor, honoring the panel filter
  const questMarkers = useMemo(() => {
    if (!map || !svgSize || !mapId) return []
    const text = questFilter.text.trim().toLowerCase()
    const out = []
    for (const q of quests) {
      const completed = questStatus[q.id] === 'completed'
      if (!questFilter.showCompleted && completed) continue
      if (
        text &&
        !(q.titleZh || '').toLowerCase().includes(text) &&
        !q.title.toLowerCase().includes(text) &&
        !(q.titleRu || '').toLowerCase().includes(text)
      )
        continue
      q.objectives.forEach((o, i) => {
        if (o.map !== mapId) return
        const objFloor = floorForY(map, o.wy)
        if (objFloor && curFloor && objFloor !== curFloor) return
        const [px, py] = project(map, { x: o.wx, y: o.wy, z: o.wz }, svgSize)
        out.push({
          key: `${q.id}:${i}`,
          quest: q,
          objective: o,
          completed,
          px,
          py,
        })
      })
    }
    return out
  }, [quests, map, mapId, svgSize, curFloor, questFilter, questStatus])

  const onLocateObjective = useCallback(
    (o) => {
      if (!svgSize || !map) return
      const objFloor = floorForY(map, o.wy)
      if (objFloor && map.floors.includes(objFloor)) setCurFloor(objFloor)
      const [px, py] = project(map, { x: o.wx, y: o.wy, z: o.wz }, svgSize)
      setFollow(false) // 定位任务点时暂停跟随，否则下一条位置事件会把视野弹回玩家
      viewportCtl.current?.centerOn(px, py)
      setPanelOpen(false)
    },
    [svgSize, map],
  )

  const onMapChange = async (id) => {
    await postJSON('/api/ingest/map', { map: id })
  }

  const onCalibTap = useCallback(
    (svgPt) => {
      if (!position || !map) {
        message.warning('没有当前位置 — 请先在游戏内截图或用测试注入')
        return
      }
      const next = [...calibPoints, { w: horizontalOf(map, position.world), s: svgPt }]
      setCalibPoints(next)
      localStorage.setItem('calib:' + mapId, JSON.stringify(next))
    },
    [calibPoints, map, mapId, position, message],
  )

  const mapOptions = state
    ? Object.entries(state.maps).map(([id, m]) => ({ value: id, label: m.nameZh || m.name }))
    : []

  return (
    <div className="flex flex-col h-full">
      <header className="flex items-center gap-2 px-2 py-2 bg-[#1d232b] border-b border-[#2c3540] z-10">
        <Select
          className="min-w-36"
          value={mapId}
          options={mapOptions}
          onChange={onMapChange}
          placeholder="选择地图"
          showSearch
        />
        {map && map.floors.length > 1 && (
          <Select
            className="min-w-32"
            value={curFloor}
            onChange={setCurFloor}
            options={map.floors.map((f) => ({ value: f, label: f.replaceAll('_', ' ') }))}
          />
        )}
        <Badge
          status={connected ? 'success' : 'error'}
          text={<span className="text-xs text-gray-400">{connected ? '已连接' : '已断开'}</span>}
        />
        <Button className="ml-auto" icon={<MenuOutlined />} onClick={() => setPanelOpen(true)} />
      </header>

      <div className="relative flex-1 flex flex-col min-h-0">
        {!map && <div className="flex-1 flex items-center justify-center text-gray-500">等待服务器状态…</div>}
        {map && !svgText && !svgError && (
          <div className="flex-1 flex items-center justify-center">
            <Spin tip="地图加载中…" />
          </div>
        )}
        {map && svgError && (
          <div className="flex-1 flex items-center justify-center text-red-400 text-center px-6 whitespace-pre-wrap">
            {`地图加载失败：${svgError}\nSVG 来源：${svgUrl}\n（可用 -svg-base 指向自建镜像）`}
          </div>
        )}
        {map && svgText && (
          <MapViewport
            ref={viewportCtl}
            map={{ ...map, id: mapId }}
            svgText={svgText}
            floor={curFloor}
            position={position}
            history={state.history}
            calibMode={calibMode}
            calibPoints={calibPoints}
            onCalibTap={onCalibTap}
            follow={follow}
            onSvgSize={setSvgSize}
            questMarkers={questMarkers}
            onQuestStatus={onQuestStatus}
          />
        )}

        <div className="absolute right-3 bottom-3 flex flex-col gap-2">
          <Tooltip title="放大" placement="left">
            <Button shape="circle" size="large" icon={<PlusOutlined />}
              onClick={() => viewportCtl.current?.zoomCenter(1.4)} />
          </Tooltip>
          <Tooltip title="缩小" placement="left">
            <Button shape="circle" size="large" icon={<MinusOutlined />}
              onClick={() => viewportCtl.current?.zoomCenter(1 / 1.4)} />
          </Tooltip>
          <Tooltip title="适配全图" placement="left">
            <Button shape="circle" size="large" icon={<CompressOutlined />}
              onClick={() => viewportCtl.current?.fitView()} />
          </Tooltip>
          <Tooltip title={follow ? '跟随玩家：开' : '跟随玩家：关'} placement="left">
            <Button shape="circle" size="large" type={follow ? 'primary' : 'default'} icon={<AimOutlined />}
              onClick={() => {
                setFollow(!follow)
                if (!follow) viewportCtl.current?.centerOnPlayer()
              }} />
          </Tooltip>
        </div>
      </div>

      <Drawer
        title="面板"
        placement={isDesktop ? 'right' : 'bottom'}
        width={380}
        height="60%"
        open={panelOpen}
        onClose={() => setPanelOpen(false)}
      >
        <SidePanel
          position={position}
          settings={state?.settings}
          map={map}
          mapId={mapId}
          svgSize={svgSize}
          calibMode={calibMode}
          setCalibMode={setCalibMode}
          calibPoints={calibPoints}
          setCalibPoints={setCalibPoints}
          quests={quests}
          questStatus={questStatus}
          questFilter={questFilter}
          setQuestFilter={setQuestFilter}
          onLocateObjective={onLocateObjective}
          onQuestStatus={onQuestStatus}
        />
      </Drawer>
    </div>
  )
}
