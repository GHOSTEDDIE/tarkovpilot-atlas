import { useState } from 'react'
import { Button, Card, Checkbox, Input, InputNumber, Space, Switch, Tag, Typography, App as AntApp } from 'antd'
import { AimOutlined, CheckOutlined, ClearOutlined, SaveOutlined, UndoOutlined } from '@ant-design/icons'
import { postJSON } from '../hooks/useServerState'
import { fitAffineFromPoints } from '../lib/projection'

const { Text, Paragraph } = Typography

function SettingsCard({ settings }) {
  const { message } = AntApp.useApp()
  const clean = !!settings?.cleanScreenshots
  return (
    <Card size="small" title="设置">
      <div className="flex items-start gap-2">
        <Switch
          checked={clean}
          onChange={async (v) => {
            const r = await postJSON('/api/config', { cleanScreenshots: v })
            r.ok
              ? message.success(v ? '已开启：进新图时删除本次会话截图' : '已关闭')
              : message.error('保存失败')
          }}
        />
        <div>
          <div className="text-sm">进新地图时删除本次会话截图</div>
          <div className="text-xs text-gray-500">
            只删除 agent 启动之后生成的 .png 截图（与 TarkovPilot 行为一致），不动旧文件
          </div>
        </div>
      </div>
    </Card>
  )
}

function QuestCard({ quests, mapId, questStatus, filter, setFilter, onLocate, onQuestStatus }) {
  const onThisMap = quests
    .map((q) => ({ ...q, objectives: q.objectives.filter((o) => o.map === mapId) }))
    .filter((q) => q.objectives.length > 0)

  const text = filter.text.trim().toLowerCase()
  const shown = onThisMap.filter((q) => {
    const completed = questStatus[q.id] === 'completed'
    if (!filter.showCompleted && completed) return false
    if (
      text &&
      !(q.titleZh || '').toLowerCase().includes(text) &&
      !q.title.toLowerCase().includes(text) &&
      !(q.titleRu || '').toLowerCase().includes(text)
    )
      return false
    return true
  })

  return (
    <Card size="small" title={`任务目标（本图 ${onThisMap.length} 个任务）`}>
      <Space direction="vertical" className="w-full">
        <Space.Compact block>
          <Input
            placeholder="搜索任务名…"
            value={filter.text}
            onChange={(e) => setFilter({ ...filter, text: e.target.value })}
            allowClear
          />
        </Space.Compact>
        <Checkbox
          checked={filter.showCompleted}
          onChange={(e) => setFilter({ ...filter, showCompleted: e.target.checked })}
        >
          显示已完成
        </Checkbox>
        <div className="max-h-64 overflow-y-auto">
          {shown.map((q) => {
            const completed = questStatus[q.id] === 'completed'
            return (
              <div key={q.id} className="flex items-center gap-2 py-1 border-b border-[#2c3540]">
                <div className="flex-1 min-w-0">
                  <div className={`text-sm truncate ${completed ? 'line-through text-gray-500' : ''}`}>
                    {q.titleZh || q.title}
                  </div>
                  <div className="text-xs text-gray-500">
                    {q.titleZh && q.title !== q.titleZh ? `${q.title} · ` : ''}
                    {q.giverZh || q.giver} · {q.objectives.length} 个目标点
                  </div>
                </div>
                <Button size="small" icon={<AimOutlined />} onClick={() => onLocate(q.objectives[0])} />
                <Button
                  size="small"
                  type={completed ? 'default' : 'primary'}
                  icon={<CheckOutlined />}
                  onClick={() => onQuestStatus(q.id, completed ? '' : 'completed')}
                />
              </div>
            )
          })}
          {shown.length === 0 && <Text type="secondary" className="text-xs">没有匹配的任务</Text>}
        </div>
      </Space>
    </Card>
  )
}

function PositionCard({ position }) {
  if (!position) {
    return (
      <Card size="small" title="当前位置">
        <Text type="secondary">暂无位置数据 — 在游戏中按截图键生成截图</Text>
      </Card>
    )
  }
  return (
    <Card size="small" title="当前位置">
      <div className="font-mono text-xs leading-5">
        <div>地图: {position.mapId}</div>
        <div>
          x: {position.world.x.toFixed(2)} y: {position.world.y.toFixed(2)} z: {position.world.z.toFixed(2)}
        </div>
        <div>时间: {new Date(position.time).toLocaleString()}</div>
        <div>来源: {position.source}</div>
        {position.rawFilename && <div className="break-all">文件: {position.rawFilename}</div>}
      </div>
    </Card>
  )
}

function TestInjectCard({ mapId }) {
  const { message } = AntApp.useApp()
  const [x, setX] = useState(null)
  const [y, setY] = useState(null)
  const [z, setZ] = useState(null)
  const [filename, setFilename] = useState('')
  const [result, setResult] = useState('')

  return (
    <Card size="small" title="测试注入">
      <Space.Compact block>
        <InputNumber placeholder="x" value={x} onChange={setX} className="flex-1" />
        <InputNumber placeholder="y (高度)" value={y} onChange={setY} className="flex-1" />
        <InputNumber placeholder="z" value={z} onChange={setZ} className="flex-1" />
        <Button
          type="primary"
          onClick={async () => {
            if (x == null || z == null) return message.warning('x/z 必填')
            await postJSON('/api/ingest/position', { mapId, world: { x, y: y || 0, z } })
          }}
        >
          发送
        </Button>
      </Space.Compact>
      <Space.Compact block className="mt-2">
        <Input
          placeholder="粘贴截图文件名测试解析"
          value={filename}
          onChange={(e) => setFilename(e.target.value)}
        />
        <Button
          onClick={async () => {
            const r = await postJSON('/api/ingest/screenshot', { filename, mapId })
            setResult(JSON.stringify(r.data, null, 2))
          }}
        >
          解析
        </Button>
      </Space.Compact>
      {result && <pre className="mt-2 text-xs whitespace-pre-wrap break-all">{result}</pre>}
    </Card>
  )
}

function CalibrationCard({ map, mapId, svgSize, calibMode, setCalibMode, calibPoints, setCalibPoints }) {
  const { message } = AntApp.useApp()
  const p = map?.projection

  const persist = (pts) => {
    setCalibPoints(pts)
    localStorage.setItem('calib:' + mapId, JSON.stringify(pts))
  }

  const fit = async () => {
    if (calibPoints.length < 3) return message.warning('至少需要 3 个校准点')
    const { affine, rms, count } = fitAffineFromPoints(map, calibPoints, svgSize)
    const next = { ...p, affine, calibrated: true, calibrationError: rms, calibrationPoints: count }
    const r = await postJSON(`/api/maps/${mapId}/projection`, next)
    if (r.ok) message.success(`已保存，RMS 误差 ${rms.toFixed(1)} px（${count} 点）`)
    else message.error('保存失败')
  }

  return (
    <Card
      size="small"
      title={
        <>
          投影校准{' '}
          {p?.calibrated ? (
            <Tag color="success">已校准 err={(p.calibrationError || 0).toFixed(1)}px</Tag>
          ) : (
            <Tag color="error">未校准</Tag>
          )}
        </>
      }
    >
      <Paragraph type="secondary" className="!text-xs">
        开启校准模式后，点击地图上你实际所在的位置（需先有游戏内截图定位），采集 ≥3 个点后拟合。
      </Paragraph>
      <Space wrap>
        <Button
          icon={<AimOutlined />}
          type={calibMode ? 'primary' : 'default'}
          onClick={() => setCalibMode(!calibMode)}
        >
          校准模式
        </Button>
        <Button icon={<SaveOutlined />} onClick={fit}>
          拟合
        </Button>
        <Button
          icon={<UndoOutlined />}
          onClick={() => persist(calibPoints.slice(0, -1))}
        >
          撤销点
        </Button>
        <Button
          icon={<ClearOutlined />}
          danger
          onClick={() => {
            persist([])
            localStorage.removeItem('calib:' + mapId)
          }}
        >
          清空
        </Button>
      </Space>
      <div className="mt-2 font-mono text-xs whitespace-pre-wrap text-gray-400">
        {calibPoints.length === 0
          ? '暂无校准点'
          : calibPoints
              .map(
                (pt, i) =>
                  `#${i + 1} 世界(${pt.w[0].toFixed(1)}, ${pt.w[1].toFixed(1)}) → 像素(${pt.s[0].toFixed(1)}, ${pt.s[1].toFixed(1)})`,
              )
              .join('\n')}
      </div>
    </Card>
  )
}

function FloorRangesCard({ map, mapId }) {
  const { message } = AntApp.useApp()
  // local editable copy: {floor: {minY, maxY}}
  const [ranges, setRanges] = useState(() => {
    const o = {}
    for (const r of map.floorRanges || []) o[r.floor] = { minY: r.minY, maxY: r.maxY }
    return o
  })

  const save = async () => {
    const out = Object.entries(ranges)
      .filter(([, v]) => v.minY != null && v.maxY != null)
      .map(([floor, v]) => ({ floor, minY: v.minY, maxY: v.maxY }))
    const r = await postJSON(`/api/maps/${mapId}/floors`, out)
    r.ok ? message.success('楼层范围已保存') : message.error('保存失败')
  }

  if (!map.floors || map.floors.length <= 1) return null
  return (
    <Card size="small" title="楼层高度范围 (worldY)">
      {map.floors.map((f) => (
        <div key={f} className="flex items-center gap-2 mb-1">
          <span className="w-32 truncate text-xs text-gray-400">{f.replaceAll('_', ' ')}</span>
          <InputNumber
            size="small"
            placeholder="minY"
            value={ranges[f]?.minY ?? null}
            onChange={(v) => setRanges({ ...ranges, [f]: { ...ranges[f], minY: v } })}
          />
          <InputNumber
            size="small"
            placeholder="maxY"
            value={ranges[f]?.maxY ?? null}
            onChange={(v) => setRanges({ ...ranges, [f]: { ...ranges[f], maxY: v } })}
          />
        </div>
      ))}
      <Button size="small" type="primary" className="mt-2" onClick={save}>
        保存楼层范围
      </Button>
    </Card>
  )
}

export default function SidePanel(props) {
  return (
    <Space direction="vertical" size="middle" className="w-full">
      <PositionCard position={props.position} />
      <SettingsCard settings={props.settings} />
      <QuestCard
        quests={props.quests}
        mapId={props.mapId}
        questStatus={props.questStatus}
        filter={props.questFilter}
        setFilter={props.setQuestFilter}
        onLocate={props.onLocateObjective}
        onQuestStatus={props.onQuestStatus}
      />
      <TestInjectCard mapId={props.mapId} />
      <CalibrationCard {...props} />
      {props.map && <FloorRangesCard key={props.mapId} map={props.map} mapId={props.mapId} />}
    </Space>
  )
}
