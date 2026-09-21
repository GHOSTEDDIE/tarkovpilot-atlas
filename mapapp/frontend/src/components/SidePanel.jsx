import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Checkbox,
  Collapse,
  Input,
  InputNumber,
  Select,
  Space,
  Switch,
  Tag,
  Typography,
  App as AntApp,
} from 'antd'
import { AimOutlined, CheckOutlined, SaveOutlined, UndoOutlined } from '@ant-design/icons'
import { postJSON } from '../hooks/useServerState'
import { fitAffineFromPoints } from '../lib/projection'

const { Text } = Typography

const statusLabels = {
  untracked: '未跟踪',
  started: '进行中',
  failed: '失败',
  completed: '已完成',
}

function SettingsCard({ settings }) {
  const { message } = AntApp.useApp()
  const clean = !!settings?.cleanScreenshots
  return (
    <Card size="small" title="设置">
      <div className="flex items-center justify-between gap-3">
        <span className="text-sm">进入新地图时清理本次会话截图</span>
        <Switch
          checked={clean}
          onChange={async (value) => {
            const result = await postJSON('/api/config', { cleanScreenshots: value })
            result.ok ? message.success('设置已保存') : message.error('保存失败')
          }}
        />
      </div>
    </Card>
  )
}

function UpdateCard({ update, catalogVersion, mapAssetVersion }) {
  const formatTime = (value) => value ? new Date(value).toLocaleString() : '尚未完成'
  const failed = update?.contentError || update?.mapAssetError
  return (
    <Card
      size="small"
      title="数据更新"
      extra={<Tag color={!update?.enabled ? 'default' : update?.checking ? 'processing' : failed ? 'warning' : 'success'}>
        {!update?.enabled ? '已关闭' : update?.checking ? '检查中' : failed ? '使用本地数据' : '正常'}
      </Tag>}
    >
      <div className="text-xs leading-5 text-gray-400">
        <div>内容数据：{catalogVersion || update?.contentVersion || '内置版本'}</div>
        <div>地图资源：{mapAssetVersion || update?.mapAssetVersion || '内置版本'}</div>
        <div>上次成功：{formatTime(update?.lastSuccess)}</div>
        {update?.enabled && <div>下次检查：{formatTime(update?.nextCheck)}</div>}
      </div>
      {failed && (
        <Alert
          className="mt-2"
          type="warning"
          showIcon
          message="在线更新失败，当前继续使用本地有效数据"
          description="程序会按计划自动重试"
        />
      )}
    </Card>
  )
}

function ProfileCard({ scope, onChange }) {
	const { message } = AntApp.useApp()
	const [profile, setProfile] = useState(scope?.profile || 'default')
	const [wipe, setWipe] = useState(scope?.wipe || 'current')
	useEffect(() => {
	  setProfile(scope?.profile || 'default')
	  setWipe(scope?.wipe || 'current')
	}, [scope?.profile, scope?.wipe])
	return (
	  <Card size="small" title="任务档案">
		<Space direction="vertical" className="w-full">
		  <Input addonBefore="档案" value={profile} onChange={(event) => setProfile(event.target.value)} />
		  <Input addonBefore="删档周期" value={wipe} onChange={(event) => setWipe(event.target.value)} />
		  <Button type="primary" onClick={async () => {
			const result = await onChange({ ...(scope || {}), profile, wipe })
			if (result?.ok) message.success('任务档案已切换')
		  }}>切换档案</Button>
		</Space>
	  </Card>
	)
}

function LayerCard({ layers, setLayers, coverage }) {
  const options = [
    ['pmc', 'PMC 撤离'],
    ['scav', 'Scav 撤离'],
    ['shared', '共享撤离'],
    ['transit', '地图转场'],
    ['tasks', '任务目标'],
  ]
  return (
    <Card
      size="small"
      title="地图图层"
      extra={coverage && <Text type="secondary" className="text-xs">撤离 {coverage.extractCount} · 转场 {coverage.transitCount}</Text>}
    >
      <div className="grid grid-cols-2 gap-2">
        {options.map(([key, label]) => (
          <Checkbox key={key} checked={layers[key]} onChange={(event) => setLayers({ ...layers, [key]: event.target.checked })}>
            {label}
          </Checkbox>
        ))}
      </div>
      {coverage?.status === 'missing' && <Alert className="mt-3" type="warning" showIcon message={coverage.messageZh || '公开数据暂缺'} />}
    </Card>
  )
}

function TaskGraphCard({
  tasks,
  filteredTasks,
  mapId,
  taskStatus,
  filter,
  setFilter,
  onLocate,
  onStatus,
  onOpen,
}) {
  const traders = useMemo(() => {
    const seen = new Map()
    for (const task of tasks) {
      if (task.traderId) seen.set(task.traderId, task.traderZh || task.trader || task.traderId)
    }
    return [...seen.entries()].map(([value, label]) => ({ value, label })).sort((a, b) => a.label.localeCompare(b.label))
  }, [tasks])

  const firstMapTarget = (task) => {
    for (const objective of task.objectives || []) {
      const location = (objective.locations || []).find((item) => item.mapId === mapId)
      if (location) return { objective, location }
    }
    return null
  }

  return (
    <Card size="small" title={`任务图谱（${filteredTasks.length}）`}>
      <Space direction="vertical" className="w-full" size="small">
        <Input placeholder="搜索任务或商人" allowClear value={filter.text} onChange={(event) => setFilter({ ...filter, text: event.target.value })} />
        <div className="grid grid-cols-2 gap-2">
          <Select
            value={filter.status}
            onChange={(status) => setFilter({ ...filter, status })}
            options={[
              { value: 'unfinished', label: '未完成' },
              { value: 'all', label: '全部状态' },
              { value: 'started', label: '进行中' },
              { value: 'failed', label: '失败' },
              { value: 'completed', label: '已完成' },
              { value: 'untracked', label: '未跟踪' },
            ]}
          />
          <Select
            value={filter.trader || undefined}
            onChange={(trader) => setFilter({ ...filter, trader: trader || '' })}
            options={traders}
            placeholder="全部商人"
            allowClear
            showSearch
            optionFilterProp="label"
          />
        </div>
        <Space wrap>
          <Checkbox checked={filter.allMaps} onChange={(event) => setFilter({ ...filter, allMaps: event.target.checked })}>全部地图</Checkbox>
          <Checkbox checked={filter.storyline} onChange={(event) => setFilter({ ...filter, storyline: event.target.checked })}>剧情主线</Checkbox>
          <Checkbox checked={filter.kappa} onChange={(event) => setFilter({ ...filter, kappa: event.target.checked })}>Kappa</Checkbox>
          <Checkbox checked={filter.lightkeeper} onChange={(event) => setFilter({ ...filter, lightkeeper: event.target.checked })}>Lightkeeper</Checkbox>
        </Space>

        <div className="task-list overflow-y-auto">
          {filteredTasks.map((task) => {
            const status = taskStatus[task.id]?.status || 'untracked'
            const target = firstMapTarget(task)
            const firstObjective = target?.objective || task.objectives?.[0]
            return (
              <div key={task.id} className="task-row border-b border-[#2c3540] py-2">
                <button className="task-title text-left w-full" onClick={() => onOpen(task, firstObjective, target?.location)}>
                  <div className={`text-sm ${status === 'completed' ? 'line-through text-gray-500' : ''}`}>{task.nameZh || task.name}</div>
                  <div className="text-xs text-gray-500 mt-0.5">
                    {task.traderZh || task.trader || '未知商人'} · 前置 {(task.requirements || []).length} · 目标 {(task.objectives || []).length}
                  </div>
                </button>
                <div className="flex items-center gap-1 mt-2">
                  <Tag color={status === 'completed' ? 'success' : status === 'failed' ? 'error' : status === 'started' ? 'processing' : 'default'}>
                    {statusLabels[status]}
                  </Tag>
                  {task.category === 'storyline' && <Tag color="blue">剧情主线</Tag>}
                  {task.kappaRequired && <Tag color="gold">Kappa</Tag>}
                  {task.lightkeeperRequired && <Tag color="purple">Lightkeeper</Tag>}
                  <div className="ml-auto flex gap-1">
                    {target && <Button size="small" icon={<AimOutlined />} onClick={() => onLocate(task, target.objective, target.location)} />}
                    <Button
                      size="small"
                      type={status === 'completed' ? 'default' : 'primary'}
                      icon={<CheckOutlined />}
                      onClick={() => onStatus(task.id, status === 'completed' ? 'untracked' : 'completed')}
                    />
                  </div>
                </div>
              </div>
            )
          })}
          {filteredTasks.length === 0 && <Text type="secondary" className="text-xs">没有匹配的任务</Text>}
        </div>
      </Space>
    </Card>
  )
}

function ReplayCard({ mode }) {
  const { message } = AntApp.useApp()
  const [preview, setPreview] = useState(null)
  const [loading, setLoading] = useState(false)
	const [breakpoint, setBreakpoint] = useState('')
	const queryFor = (selected = breakpoint) => {
	  const query = new URLSearchParams({ mode })
	  if (selected) {
		query.set('fromSession', selected)
		const item = (preview?.breakpoints || []).find((value) => value.sessionId === selected)
		if (item?.gameProfileId) query.set('sourceProfile', item.gameProfileId)
	  }
	  return query.toString()
	}

  const scan = async (selected = breakpoint) => {
    setLoading(true)
    try {
	  const response = await fetch(`/api/log-replay/candidates?${queryFor(selected)}`)
      const body = await response.json()
      if (!response.ok) throw new Error(body.error || '扫描失败')
	  setPreview(body)
	  if (!breakpoint && body.breakpoints?.length === 1) setBreakpoint(body.breakpoints[0].sessionId)
    } catch (error) {
      message.warning(error.message)
    } finally {
      setLoading(false)
    }
  }

  const apply = async () => {
    setLoading(true)
	const result = await postJSON(`/api/log-replay?${queryFor()}`, {})
    setLoading(false)
    if (!result.ok) return message.error(result.data?.error || '补录失败')
    setPreview(result.data)
    message.success(`已应用 ${result.data.changeCount} 项状态变化`)
  }

  return (
    <Card size="small" title="历史任务日志">
      <Space direction="vertical" className="w-full">
		<Button loading={loading} onClick={() => scan()}>扫描历史日志</Button>
        {preview && (
		  <>
			{(preview.breakpoints || []).length > 0 && (
			  <Select
				className="w-full"
				placeholder="选择当前删档周期的日志起点"
				value={breakpoint || undefined}
				onChange={(value) => {
				  setBreakpoint(value)
				  scan(value)
				}}
				options={preview.breakpoints.map((item) => ({
				  value: item.sessionId,
				  label: `${item.sessionId} · ${item.mode || '未知模式'} · ${item.gameVersion || '未知版本'} · ${item.gameProfileId || '未知档案'}`,
				}))}
			  />
			)}
			<Alert
            type={preview.changeCount > 0 ? 'info' : 'success'}
            message={`${preview.events} 个事件，${preview.changeCount} 项状态变化`}
			description={`${preview.sessions} 个会话 · 重复 ${preview.duplicates} · 跳过 ${preview.skipped} · 其他模式 ${preview.modeSkipped || 0}`}
			action={preview.changeCount > 0 && !preview.applied && breakpoint ? <Button size="small" type="primary" onClick={apply}>确认补录</Button> : null}
          />
		  </>
        )}
      </Space>
    </Card>
  )
}

function PositionCard({ position }) {
  return (
    <Card size="small" title="当前位置">
      {position ? (
        <div className="font-mono text-xs leading-5">
          <div>{position.mapId}</div>
          <div>{position.world.x.toFixed(2)}, {position.world.y.toFixed(2)}, {position.world.z.toFixed(2)}</div>
          <div>{new Date(position.time).toLocaleString()}</div>
        </div>
      ) : <Text type="secondary">暂无位置数据</Text>}
    </Card>
  )
}

function MapTools({ map, mapId, svgSize, calibMode, setCalibMode, calibPoints, setCalibPoints }) {
  const { message } = AntApp.useApp()
  const [x, setX] = useState(null)
  const [y, setY] = useState(null)
  const [z, setZ] = useState(null)
  if (!map) return null

  const persistPoints = (points) => {
    setCalibPoints(points)
    localStorage.setItem(`calib:${mapId}`, JSON.stringify(points))
  }
  const fit = async () => {
    if (calibPoints.length < 3) return message.warning('至少需要 3 个校准点')
    const result = fitAffineFromPoints(map, calibPoints, svgSize)
    const projection = {
      ...map.projection,
      affine: result.affine,
      calibrated: true,
      calibrationError: result.rms,
      calibrationPoints: result.count,
    }
    const saved = await postJSON(`/api/maps/${mapId}/projection`, projection)
    saved.ok ? message.success('地图校准已保存') : message.error('保存失败')
  }

  return (
    <Collapse
      size="small"
      items={[{
        key: 'map-tools',
        label: '定位与地图校准',
        children: (
          <Space direction="vertical" className="w-full">
            <Space wrap>
              <Button type={calibMode ? 'primary' : 'default'} icon={<AimOutlined />} onClick={() => setCalibMode(!calibMode)}>采集校准点</Button>
              <Button icon={<SaveOutlined />} onClick={fit}>保存校准</Button>
              <Button icon={<UndoOutlined />} onClick={() => persistPoints(calibPoints.slice(0, -1))}>撤销</Button>
            </Space>
            <Text type="secondary" className="text-xs">已采集 {calibPoints.length} 个点</Text>
            <Space.Compact block>
              <InputNumber placeholder="x" value={x} onChange={setX} className="flex-1" />
              <InputNumber placeholder="y" value={y} onChange={setY} className="flex-1" />
              <InputNumber placeholder="z" value={z} onChange={setZ} className="flex-1" />
              <Button onClick={() => postJSON('/api/ingest/position', { mapId, world: { x, y: y || 0, z } })}>定位</Button>
            </Space.Compact>
          </Space>
        ),
      }]}
    />
  )
}

export default function SidePanel(props) {
  return (
    <Space direction="vertical" size="middle" className="w-full">
      <LayerCard layers={props.layers} setLayers={props.setLayers} coverage={props.coverage} />
      <TaskGraphCard
        tasks={props.tasks}
        filteredTasks={props.filteredTasks}
        mapId={props.mapId}
        taskStatus={props.taskStatus}
        filter={props.taskFilter}
        setFilter={props.setTaskFilter}
        onLocate={props.onLocateObjective}
        onStatus={props.onTaskStatus}
        onOpen={props.onOpenTask}
      />
      <ReplayCard mode={props.mode} />
	  <ProfileCard scope={props.activeScope} onChange={props.onScopeChange} />
      <PositionCard position={props.position} />
      <SettingsCard settings={props.settings} />
      <UpdateCard update={props.update} catalogVersion={props.catalogVersion} mapAssetVersion={props.mapAssetVersion} />
      <MapTools {...props} />
      {props.meta && <Text type="secondary" className="text-xs">内容包 {props.meta.version}</Text>}
    </Space>
  )
}
