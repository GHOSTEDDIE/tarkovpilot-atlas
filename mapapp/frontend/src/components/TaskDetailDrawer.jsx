import { useState } from 'react'
import { Button, Carousel, Checkbox, Descriptions, Drawer, Empty, Image, Space, Tag, Typography } from 'antd'

const { Link, Paragraph, Text, Title } = Typography

const statusLabels = {
  untracked: '未跟踪',
  started: '进行中',
  failed: '失败',
  completed: '已完成',
}

const mapLabels = {
  customs: '海关',
  factory: '工厂',
  groundzero: '中心区',
  interchange: '立交桥',
  lab: '实验室',
  labyrinth: '迷宫',
  icebreaker: '破冰船',
  lighthouse: '灯塔',
  reserve: '储备站',
  shoreline: '海岸线',
  streetsoftarkov: '塔科夫街区',
  terminal: '航站楼',
  woods: '森林',
}

function TaskCover({ src, alt }) {
  const [failed, setFailed] = useState(false)
  if (!src || failed) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无现场截图" />
  return <img className="task-cover" src={src} alt={alt} onError={() => setFailed(true)} />
}

export default function TaskDetailDrawer({
  open,
  onClose,
  selection,
  taskById,
  successors,
  taskStatus,
  objectiveStatus,
  onTaskStatus,
  onObjectiveStatus,
}) {
  const task = selection?.task
  const objective = selection?.objective
  if (!task) return null
  const status = taskStatus?.[task.id]?.status || 'untracked'
  const completed = status === 'completed'
  const objectiveProgressId = objective?.progressId || objective?.id
  const objectiveCompleted = completed || objectiveStatus?.[objectiveProgressId]?.status === 'completed'
  const prerequisiteTasks = (task.requirements || []).map((r) => taskById.get(r.taskId)).filter(Boolean)
  const nextTasks = (successors.get(task.id) || []).map((id) => taskById.get(id)).filter(Boolean)
  const media = objective?.media || []

  return (
    <Drawer title="任务详情" width={480} open={open} onClose={onClose}>
      <Space direction="vertical" size="middle" className="w-full">
        <div>
          <Title level={4} className="!mb-1">{task.nameZh || task.name}</Title>
          <Space wrap>
            <Tag>{task.traderZh || task.trader || '未知商人'}</Tag>
            <Tag color={status === 'completed' ? 'success' : status === 'failed' ? 'error' : status === 'started' ? 'processing' : 'default'}>
              {statusLabels[status] || status}
            </Tag>
            {task.kappaRequired && <Tag color="gold">Kappa</Tag>}
            {task.lightkeeperRequired && <Tag color="purple">Lightkeeper</Tag>}
            {task.category === 'storyline' && <Tag color="blue">剧情主线</Tag>}
          </Space>
          {(task.descriptionZh || task.description) && (
            <Paragraph className="!mt-3 !mb-0 text-gray-400">{task.descriptionZh || task.description}</Paragraph>
          )}
        </div>

        <Space wrap>
          <Button onClick={() => onTaskStatus(task.id, 'started')}>标记进行中</Button>
          <Button danger onClick={() => onTaskStatus(task.id, 'failed')}>标记失败</Button>
          <Button type="primary" onClick={() => onTaskStatus(task.id, 'completed')}>标记完成</Button>
          <Button onClick={() => onTaskStatus(task.id, 'untracked')}>取消跟踪</Button>
        </Space>

        {task.category === 'storyline' ? (
          <section>
            <Text type="secondary">剧情目标</Text>
            <div className="storyline-objectives mt-2 space-y-2">
              {(task.objectives || []).map((item, index) => {
                const progressId = item.progressId || `${task.id}:${item.id}`
                const itemCompleted = completed || objectiveStatus?.[progressId]?.status === 'completed'
                return (
                  <div key={progressId} className="rounded border border-[#2c3540] px-3 py-2">
                    <Checkbox
                      checked={itemCompleted}
                      disabled={completed}
                      onChange={(event) => onObjectiveStatus(progressId, event.target.checked)}
                    >
                      <span className={itemCompleted ? 'line-through text-gray-500' : ''}>
                        {index + 1}. {item.descriptionZh || item.description}
                      </span>
                    </Checkbox>
                    <div className="mt-1 pl-6">
                      {item.optional && <Tag>可选</Tag>}
                      {(item.maps || []).map((mapId) => <Tag key={mapId}>{mapLabels[mapId] || mapId}</Tag>)}
                    </div>
                  </div>
                )
              })}
              {(task.objectives || []).length === 0 && <Text type="secondary">该章节按剧情选择推进</Text>}
            </div>
          </section>
        ) : objective && (
          <section>
            <Text type="secondary">当前目标</Text>
            <Paragraph className="!mt-1 !mb-2">{objective.descriptionZh || objective.description}</Paragraph>
            <Checkbox
              checked={objectiveCompleted}
              disabled={completed}
              onChange={(event) => onObjectiveStatus(objectiveProgressId, event.target.checked)}
            >
              {completed ? '任务完成，目标视为已完成' : '手动标记此目标完成'}
            </Checkbox>
          </section>
        )}

		{selection.location && (
		  <div className="text-xs text-gray-400">
			坐标：{selection.location.position.x.toFixed(1)}, {selection.location.position.y.toFixed(1)}, {selection.location.position.z.toFixed(1)}
			{selection.location.floorPending && <span className="ml-2 text-amber-400">楼层待确认</span>}
		  </div>
		)}

        {objective && (
          <section>
            <Text type="secondary">游戏内位置截图</Text>
            {media.length > 0 ? (
              <Carousel dots className="mt-2 task-media-carousel">
                {media.map((item) => (
                  <div key={`${item.localFile}:${item.checksum}`}>
                    <Image className="task-media" src={`/media/${item.localFile}`} alt={item.captionZh || objective.descriptionZh} />
                    <div className="px-1 py-2 text-xs text-gray-400">
                      <div>{item.captionZh || '任务位置'}</div>
                      <div>
                        来源：<a href={item.sourceUrl} target="_blank" rel="noreferrer">{item.author}</a> · {item.license}
                      </div>
                    </div>
                  </div>
                ))}
              </Carousel>
            ) : (
              <div className="mt-2 rounded border border-[#2c3540] p-3">
                <TaskCover key={task.id} src={task.taskImageLink} alt={task.nameZh || task.name} />
                <div className="mt-2 text-xs text-gray-400">暂无经过审核的现场截图</div>
              </div>
            )}
          </section>
        )}

        <Descriptions size="small" column={1} bordered>
          <Descriptions.Item label="前置任务">
            {prerequisiteTasks.length ? prerequisiteTasks.map((item) => item.nameZh || item.name).join('、') : '无'}
          </Descriptions.Item>
          <Descriptions.Item label="后续任务">
            {nextTasks.length ? nextTasks.map((item) => item.nameZh || item.name).join('、') : '无'}
          </Descriptions.Item>
          <Descriptions.Item label="最低等级">{task.minPlayerLevel || '无要求'}</Descriptions.Item>
        </Descriptions>

        {task.wikiLink && (
          <Link href={task.wikiLink} target="_blank" rel="noreferrer">打开任务 Wiki</Link>
        )}
      </Space>
    </Drawer>
  )
}
