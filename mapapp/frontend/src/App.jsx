import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Badge, Button, Drawer, Grid, Select, Spin, Tooltip, App as AntApp } from 'antd'
import {
  AimOutlined,
  CompressOutlined,
  MenuOutlined,
  MinusOutlined,
  PlusOutlined,
} from '@ant-design/icons'
import { useCatalog, useServerState, useSvgText, postJSON, putJSON } from './hooks/useServerState'
import { horizontalOf, project } from './lib/projection'
import { buildFallbackSVG, buildRasterSVG } from './lib/rasterMap'
import MapViewport from './components/MapViewport'
import SidePanel from './components/SidePanel'
import TaskDetailDrawer from './components/TaskDetailDrawer'

function floorForY(map, y) {
  for (const range of map.floorRanges || []) {
    if (y >= range.minY && y < range.maxY) return range.floor
  }
  return ''
}

function taskOnMap(task, mapId) {
  if (task.mapId === mapId) return true
  return (task.objectives || []).some(
    (objective) =>
      (objective.maps || []).includes(mapId) ||
      (objective.locations || []).some((location) => location.mapId === mapId),
  )
}

function taskMatches(task, filter, status) {
  const text = filter.text.trim().toLowerCase()
  if (
    text &&
    ![task.name, task.nameZh, task.description, task.descriptionZh, task.trader, task.traderZh]
      .filter(Boolean)
      .some((value) => value.toLowerCase().includes(text))
  ) return false
  if (filter.trader && task.traderId !== filter.trader) return false
  if (filter.storyline && task.category !== 'storyline') return false
  if (filter.kappa && !task.kappaRequired) return false
  if (filter.lightkeeper && !task.lightkeeperRequired) return false
  if (filter.status === 'unfinished' && status === 'completed') return false
  if (filter.status !== 'all' && filter.status !== 'unfinished' && status !== filter.status) return false
  return true
}

export default function App() {
  const { message } = AntApp.useApp()
  const { state, connected } = useServerState()
  const screens = Grid.useBreakpoint()
  const isDesktop = !!screens.md
  const viewportCtl = useRef(null)
	const scopeInitialized = useRef(false)

  const [mode, setMode] = useState(() => localStorage.getItem('task-mode') || 'pvp')
  const [curFloor, setCurFloor] = useState(null)
  const [follow, setFollow] = useState(true)
  const [calibMode, setCalibMode] = useState(false)
  const [calibPoints, setCalibPoints] = useState([])
  const [panelOpen, setPanelOpen] = useState(false)
  const [taskSelection, setTaskSelection] = useState(null)
  const [taskFilter, setTaskFilter] = useState({
    text: '', status: 'unfinished', trader: '', storyline: false, kappa: false, lightkeeper: false, allMaps: false,
  })
  const [layers, setLayers] = useState({ pmc: true, scav: true, shared: true, transit: true, tasks: true })

  const stopFollowing = useCallback(() => setFollow(false), [])

  const mapId = state?.mapId || null
  const map = state && mapId ? state.maps[mapId] : null
  const position = state?.position && state.position.mapId === mapId ? state.position : null
  const catalog = useCatalog(mode, mapId, state?.catalogVersion)
  const currentProgress = state?.activeScope?.mode === mode ? state.progress : catalog.progress
  const taskStatus = currentProgress?.tasks || {}
  const objectiveStatus = currentProgress?.objectives || {}

  useEffect(() => {
    if (scopeInitialized.current || !state?.activeScope?.mode) return
    scopeInitialized.current = true
    setMode(state.activeScope.mode)
    localStorage.setItem('task-mode', state.activeScope.mode)
  }, [state?.activeScope?.mode])

  useEffect(() => {
    if (!mapId) return
    setCurFloor(null)
    setCalibMode(false)
    setCalibPoints(JSON.parse(localStorage.getItem(`calib:${mapId}`) || '[]'))
  }, [mapId])

  useEffect(() => {
    if (map && (!curFloor || !map.floors.includes(curFloor))) setCurFloor(map.defaultFloor)
  }, [map, curFloor])

  useEffect(() => {
    if (map && position?.floor && map.floors.includes(position.floor)) setCurFloor(position.floor)
  }, [map, position])

  const assetVersion = state?.mapAssetVersion || 'embedded'
  const svgUrl = map?.svgFile ? `${state.svgBaseUrl}${map.svgFile}?v=${encodeURIComponent(assetVersion)}` : null
  const { text: svgText, error: svgError } = useSvgText(svgUrl || 'about:blank')
	const rasterSvgText = useMemo(
		() => (map?.raster ? buildRasterSVG(map, state.svgBaseUrl, assetVersion) : null),
		[map, state?.svgBaseUrl, assetVersion],
	)
	const fallbackSvgText = useMemo(
		() => (!map?.raster && svgError ? buildFallbackSVG(map, '/maps/', assetVersion) : null),
		[map, assetVersion, svgError],
	)
	const usingStaticFallback = !!fallbackSvgText
	const mapText = rasterSvgText || svgText || fallbackSvgText
	const viewportMap = usingStaticFallback
		? { ...map, attribution: map.fallbackAttribution, attributionUrl: map.fallbackAttributionUrl }
		: map
  const [svgSize, setSvgSize] = useState(null)

  const taskById = useMemo(() => new Map(catalog.tasks.map((task) => [task.id, task])), [catalog.tasks])
  const successors = useMemo(() => {
    const result = new Map()
    for (const task of catalog.tasks) {
      for (const requirement of task.requirements || []) {
        const list = result.get(requirement.taskId) || []
        list.push(task.id)
        result.set(requirement.taskId, list)
      }
    }
    return result
  }, [catalog.tasks])

  const filteredTasks = useMemo(
    () => catalog.tasks.filter((task) => {
      if (!taskFilter.allMaps && !(taskFilter.storyline && task.category === 'storyline') && !taskOnMap(task, mapId)) return false
      return taskMatches(task, taskFilter, taskStatus[task.id]?.status || 'untracked')
    }),
    [catalog.tasks, mapId, taskFilter, taskStatus],
  )

  const taskMarkers = useMemo(() => {
    if (!map || !svgSize || !mapId || !layers.tasks) return []
    const allowed = new Set(filteredTasks.map((task) => task.id))
    const markers = []
    for (const task of catalog.tasks) {
      if (!allowed.has(task.id)) continue
      const taskCompleted = taskStatus[task.id]?.status === 'completed'
      for (const objective of task.objectives || []) {
        for (const location of objective.locations || []) {
		  if (location.retired) continue
          if (location.mapId !== mapId) continue
          const locationFloor = floorForY(map, location.position.y)
          if (locationFloor && curFloor && locationFloor !== curFloor) continue
          const [px, py] = project(map, location.position, svgSize)
          markers.push({
            key: `${task.id}:${objective.id}:${location.id}`,
            task,
            objective,
			location: {
			  ...location,
			  floorPending: location.floorPending || (map.floors.length > 1 && !locationFloor),
			},
            completed: taskCompleted || objectiveStatus[objective.progressId || objective.id]?.status === 'completed',
            px,
            py,
          })
        }
      }
    }
    return markers
  }, [map, svgSize, mapId, layers.tasks, filteredTasks, catalog.tasks, taskStatus, objectiveStatus, curFloor])

  const featureMarkers = useMemo(() => {
    if (!map || !svgSize) return []
    return catalog.features
      .filter((feature) => feature.kind === 'transit' ? layers.transit : layers[feature.faction])
      .filter((feature) => {
        const featureFloor = floorForY(map, feature.position.y)
        return !featureFloor || !curFloor || featureFloor === curFloor
      })
      .map((feature) => {
        const [px, py] = project(map, feature.position, svgSize)
		return {
		  key: `${feature.kind}:${feature.id}`,
		  feature: {
			...feature,
			floorPending: feature.floorPending || (map.floors.length > 1 && !floorForY(map, feature.position.y)),
		  },
		  px,
		  py,
		}
      })
  }, [map, svgSize, catalog.features, layers, curFloor])

	const setActiveScope = async (scope) => {
	setMode(scope.mode || mode)
	localStorage.setItem('task-mode', scope.mode || mode)
    const result = await putJSON('/api/progress/scope', scope)
	if (!result.ok) message.error('档案切换失败')
	return result
	}

  const setScopeMode = async (nextMode) => {
	await setActiveScope({ ...(state?.activeScope || {}), mode: nextMode })
  }

  const onTaskStatus = useCallback(async (taskId, status) => {
    const result = await putJSON(`/api/progress/tasks/${taskId}`, { status, mode })
    if (!result.ok) message.error('任务状态保存失败')
  }, [mode, message])

  const onObjectiveStatus = useCallback(async (objectiveId, completed) => {
    const result = await putJSON(`/api/progress/objectives/${encodeURIComponent(objectiveId)}`, { completed, mode })
    if (!result.ok) message.error('目标状态保存失败')
  }, [mode, message])

  const onLocateObjective = useCallback((task, objective, location) => {
    if (!svgSize || !map || !location) return
	if (usingStaticFallback) {
	  message.warning('2D 参考图未标定，无法定位任务坐标')
	  return
	}
    const objectiveFloor = floorForY(map, location.position.y)
    if (objectiveFloor && map.floors.includes(objectiveFloor)) setCurFloor(objectiveFloor)
    const [px, py] = project(map, location.position, svgSize)
    setFollow(false)
    viewportCtl.current?.centerOn(px, py)
    setPanelOpen(false)
    setTaskSelection({ task, objective, location })
  }, [svgSize, map, usingStaticFallback, message])

  const onMapChange = async (id) => postJSON('/api/ingest/map', { map: id })

  const onCalibTap = useCallback((svgPoint) => {
    if (!position || !map) {
      message.warning('请先获取当前位置')
      return
    }
    const next = [...calibPoints, { w: horizontalOf(map, position.world), s: svgPoint }]
    setCalibPoints(next)
    localStorage.setItem(`calib:${mapId}`, JSON.stringify(next))
  }, [calibPoints, map, mapId, position, message])

  const mapOptions = state
    ? Object.entries(state.maps).map(([id, value]) => ({ value: id, label: value.nameZh || value.name }))
    : []

  return (
    <div className="flex flex-col h-full">
      <header className="app-header flex items-center gap-2 px-2 py-2 bg-[#1d232b] border-b border-[#2c3540] z-10">
        <Select className="map-select min-w-36" value={mapId} options={mapOptions} onChange={onMapChange} placeholder="选择地图" showSearch />
        <Select
          className="mode-select w-24"
          value={mode}
          onChange={setScopeMode}
          options={[{ value: 'pvp', label: 'PvP' }, { value: 'pve', label: 'PvE' }]}
        />
        {map && map.floors.length > 1 && (
          <Select
            className="floor-select min-w-32"
            value={curFloor}
            onChange={setCurFloor}
            options={map.floors.map((floor) => ({ value: floor, label: floor.replaceAll('_', ' ') }))}
          />
        )}
        <Badge status={connected ? 'success' : 'error'} text={<span className="text-xs text-gray-400">{connected ? '已连接' : '已断开'}</span>} />
        <Button className="ml-auto" icon={<MenuOutlined />} onClick={() => setPanelOpen(true)}>任务图谱</Button>
      </header>

      <div className="relative flex-1 flex flex-col min-h-0">
        {!map && <div className="flex-1 flex items-center justify-center text-gray-500">等待服务器状态…</div>}
        {map && (!mapText || catalog.loading) && (!svgError || !!map.fallbackImage) && <div className="flex-1 flex items-center justify-center"><Spin tip="地图加载中…" /></div>}
        {map && !map.raster && svgError && !fallbackSvgText && <div className="flex-1 flex items-center justify-center text-red-400">地图加载失败：{svgError}</div>}
        {map && mapText && !catalog.loading && (
          <MapViewport
            ref={viewportCtl}
            map={{ ...viewportMap, id: mapId }}
            svgText={mapText}
            floor={curFloor}
            position={usingStaticFallback ? null : position}
            history={usingStaticFallback ? [] : state.history}
            calibMode={usingStaticFallback ? false : calibMode}
            calibPoints={usingStaticFallback ? [] : calibPoints}
            onCalibTap={onCalibTap}
            follow={follow}
            onManualMove={stopFollowing}
            onSvgSize={setSvgSize}
            taskMarkers={usingStaticFallback ? [] : taskMarkers}
            featureMarkers={usingStaticFallback ? [] : featureMarkers}
            onOpenTask={setTaskSelection}
          />
        )}

        {usingStaticFallback && (
          <div className="absolute left-3 top-3 rounded bg-[#1d232be8] border border-amber-500/50 px-3 py-2 text-xs text-amber-300">
            交互地图不可用，正在显示 2D 参考图；位置与地图标记已暂停
          </div>
        )}

        {catalog.coverage?.status === 'missing' && (
          <div className="absolute left-3 top-3 rounded bg-[#1d232be8] border border-amber-500/50 px-3 py-2 text-xs text-amber-300">
            {catalog.coverage.messageZh || '公开数据暂缺'}
          </div>
        )}

        <div className="map-controls absolute right-3 bottom-3 flex flex-col gap-2">
          <Tooltip title="放大" placement="left"><Button shape="circle" size="large" icon={<PlusOutlined />} onClick={() => { stopFollowing(); viewportCtl.current?.zoomCenter(1.4) }} /></Tooltip>
          <Tooltip title="缩小" placement="left"><Button shape="circle" size="large" icon={<MinusOutlined />} onClick={() => { stopFollowing(); viewportCtl.current?.zoomCenter(1 / 1.4) }} /></Tooltip>
          <Tooltip title="适配全图" placement="left"><Button shape="circle" size="large" icon={<CompressOutlined />} onClick={() => { stopFollowing(); viewportCtl.current?.fitView() }} /></Tooltip>
          <Tooltip title={usingStaticFallback ? '2D 参考图不支持位置跟随' : follow ? '跟随玩家：开' : '跟随玩家：关'} placement="left">
            <Button disabled={usingStaticFallback} shape="circle" size="large" type={follow ? 'primary' : 'default'} icon={<AimOutlined />} onClick={() => {
              setFollow(!follow)
              if (!follow) viewportCtl.current?.centerOnPlayer()
            }} />
          </Tooltip>
        </div>
      </div>

      <Drawer title="任务图谱" placement={isDesktop ? 'right' : 'bottom'} width={440} height="78%" open={panelOpen} onClose={() => setPanelOpen(false)}>
        <SidePanel
          position={position}
          settings={state?.settings}
          update={state?.update}
          catalogVersion={state?.catalogVersion}
          mapAssetVersion={state?.mapAssetVersion}
          map={map}
          mapId={mapId}
          svgSize={svgSize}
          calibMode={calibMode}
          setCalibMode={setCalibMode}
          calibPoints={calibPoints}
          setCalibPoints={setCalibPoints}
          tasks={catalog.tasks}
          filteredTasks={filteredTasks}
          taskStatus={taskStatus}
          objectiveStatus={objectiveStatus}
          taskFilter={taskFilter}
          setTaskFilter={setTaskFilter}
          layers={layers}
          setLayers={setLayers}
          coverage={catalog.coverage}
          meta={catalog.meta}
          mode={mode}
		  activeScope={state?.activeScope}
		  onScopeChange={setActiveScope}
          onLocateObjective={onLocateObjective}
          onTaskStatus={onTaskStatus}
          onOpenTask={(task, objective, location) => setTaskSelection({ task, objective, location })}
        />
      </Drawer>

      <TaskDetailDrawer
        open={!!taskSelection}
        onClose={() => setTaskSelection(null)}
        selection={taskSelection}
        taskById={taskById}
        successors={successors}
        taskStatus={taskStatus}
        objectiveStatus={objectiveStatus}
        onTaskStatus={onTaskStatus}
        onObjectiveStatus={onObjectiveStatus}
      />
    </div>
  )
}
