# TarkovPilot Atlas — 自托管塔科夫地图与任务图谱

按照 `../TARKOV_MAP_POSITION_APP_PLAN.md` 实现的独立定位工具。
**不读游戏内存、不注入、不改游戏文件**：只读取 EFT 自己生成的截图文件名和游戏日志。

```
游戏 PC 上的 agent ──HTTP──> 本服务器 ──SSE──> 浏览器/手机网页
（也可用网页里的“测试注入”直接发坐标，不需要 agent）
```

## 快速开始（Windows 双击即用）

下载/编译 `tarkovpilot-atlas-windows-amd64.exe`（或 arm64），**双击运行**即可：

- 自动启动服务器 + 内置 agent（实时监听 `%USERPROFILE%\Documents\Escape from Tarkov\Screenshots`，
  自动检测常见安装路径下的游戏日志目录）；
- 自动用默认浏览器打开地图页面；
- 数据（校准、分档任务状态）保存在 exe 同目录的 `mapapp-data.json`；游戏内容默认使用程序内置快照，
  程序在后台自动更新并将已验证版本保存为同目录的 `mapapp-content.json`；地图资源保存在
  `mapapp-map-cache`。断网、下载失败或新包损坏时继续使用上一次有效版本，首次运行失败则回退内置快照；
- 手机使用：与电脑同一局域网，浏览器打开启动日志里打印的 `http://<电脑IP>:8400`。

游戏里记得用 **EFT 自带截图键**截图（Steam F12 截图不含坐标）。

## 从源码构建

```bash
cd frontend && npm install && npm run build   # 前端构建到 internal/server/static（被 Go 嵌入）
cd .. && go build -o tarkovpilot-atlas .       # 单文件程序（含网页）

# Windows 交叉编译
GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o tarkovpilot-atlas-windows-amd64.exe .
```

技术栈：后端 Go + fsnotify；前端 React 19 + Ant Design 5 + Tailwind CSS 4（Vite 构建）。

## 命令行（一般用不到）

```bash
tarkovpilot-atlas                          # 双击模式 = 下面的 serve 全默认
tarkovpilot-atlas serve [-addr :8400] [-data FILE] [-token T] [-svg-base URL] [-auto-update=true] [-update-interval 24h] [-no-browser] [-agent=false]
tarkovpilot-atlas agent -server http://<服务器IP>:8400 [-screenshots DIR] [-logs DIR]   # 游戏和服务器分开两台机器时
tarkovpilot-atlas agent replay-quests -server http://<服务器IP>:8400 -logs DIR [-profile default] [-mode pvp|pve] [-wipe current]
# 上一条只预览档案/版本候选起点；确认后追加：-source-profile ID -from-session DIR -apply
tarkovpilot-atlas data refresh [-file FILE]                         # 只预览校验报告
tarkovpilot-atlas data refresh [-file FILE] -apply                  # 确认后原子激活
tarkovpilot-atlas data maps-refresh [-dir DIR]                      # 手动刷新并激活完整地图资源包
tarkovpilot-atlas data rollback [-file FILE]                        # 回滚上一内容包
tarkovpilot-atlas parse "<截图文件名>"      # 调试图文件名解析
```

服务器参数：`-data` 状态/校准数据文件（默认 `mapapp-data.json`）；`-token` 摄入接口令牌；
`-content` 活动游戏内容包（默认 `mapapp-content.json`，不存在或校验失败时回退内置快照）；
`-svg-base` 地图 SVG 来源（默认使用本地版本缓存及程序内嵌地图，也可指向自建镜像）；
`-auto-update=false` 可关闭后台更新，`-update-interval` 和 `-update-retry` 分别控制成功检查与失败重试间隔。

手机使用：手机与服务器同一局域网，浏览器打开 `http://<服务器IP>:8400`。

## 工作方式

1. 游戏内用 **EFT 自带截图键**截图（Steam F12 截图不含坐标）；
2. agent 发现新截图 → 只把**文件名**发给服务器（不读图片内容）；
3. 服务器解析文件名中的世界坐标和相机四元数；
4. agent 同时监听日志识别当前地图（PVP `location:` / PVE `scene preset`，支持晚于进图启动）；
5. 网页实时显示玩家位置、朝向箭头、历史轨迹，支持缩放/拖拽/跟随/楼层切换。

## 撤离点与任务图谱

- 13 张底图统一显示 PMC、Scav、共享撤离点和地图转场：原有 11 张 SVG，加上迷宫单层瓦片图和
  破冰船 16 层瓦片图。工厂日/夜、Ground Zero 普通/21+/教程及实验室普通/Dark 会按变体合并；
  Terminal、Icebreaker 当前上游没有公开撤离数据，页面会明确显示缺口。
- “任务图谱”同时包含 PvP/PvE 普通商人任务和 1.0 剧情章节；“剧情主线”、Kappa、Lightkeeper
  分别筛选不同任务集合。当前剧情层包含塔科夫之旅、陨落星辰、门票、巴蒂亚、无名者、神秘蓝焰、
  他们已经来了、意外证人、迷宫和北风十个章节。
- 玩家、任务点、撤离点和转场点都使用同一套世界坐标投影。多层地图没有可靠高度范围时，标记放在默认层并提示“楼层待确认”。
- 任务状态按档案、PvP/PvE 和删档周期隔离。日志状态以通知类型 `10 → started`、`11 → failed`、
  `12 → completed` 为准，模板后缀只作兼容回退；单个目标只允许手动维护。
- 历史日志先在网页中预览变化，再确认补录；分离部署时可在游戏电脑执行 `agent replay-quests`。
- 点击任务点会打开详情抽屉。只有 `internal/content/data/media.json` 中经过来源、授权、哈希和尺寸校验的
  本地图片才会显示；缺图时显示任务封面、目标说明和 Wiki 链接，不自动抓取第三方 Wiki 图片。

数据来源：

- 任务定义与目标点坐标：json.tarkov.dev 静态数据导出（tarkov.dev 官方社区数据的镜像），
  目标点为世界坐标（含高度），与玩家位置共用同一套投影/校准管线；
- 1.0 剧情章节：独立版本化离线补充层，因 json.tarkov.dev 尚未提供这类多阶段章节；目标资料参考
  Wilsman/kappas、官方 Wiki 与中文社区资料，授权说明见 `THIRD_PARTY_NOTICES.md`。没有审核坐标的
  剧情目标只显示在任务图谱中，不生成虚假地图标记；
- 任务名/地图名/目标描述中文：游戏官方中文 locale（经 SPT 数据仓库镜像），按 BSG gameId/conditionId
  匹配（个别新任务尚无官方中文名时回退英文）；
- 内容包刷新：`tarkovpilot-atlas data refresh` 会下载 PvP/PvE 地图、任务、商人和中英文翻译，报告来源时间、
  ETag、SHA-256、数量变化、翻译/地图缺口；撤离点使用名称、阵营、空间位置和目的地图生成的本地稳定 ID，
  不再依赖会被上游重建的原始 ID。只有追加 `-apply` 才会原子切换，旧版本保存在 `.previous`。
- 当前内置内容包为 `20260921T075443Z-6e89d6e48ecf`：活动任务 PvP 525 / PvE 522（均含 10 个剧情章节），
  两种模式各有 152 个撤离点、33 个转场。上游已移除的 2 个旧活动任务和旧坐标仅作为隐藏回归资料保留，
  不进入活动任务接口或地图标记。这些数量只是当前数据结果，不是业务常量。

## 自动更新与本地兜底

服务器启动后立即使用本地数据，不等待网络；后台更新默认每 24 小时检查一次，失败后 1 小时重试：

1. 游戏内容从 `json.tarkov.dev` 下载 PvP/PvE 地图、任务、商人及中英文翻译；
2. SVG 从 `assets.tarkov.dev/maps/svg/` 更新，迷宫和破冰船瓦片、灯塔 2D 参考图按注册表中的固定来源更新；
3. 内容包继续执行结构、引用、坐标、翻译、重复 ID 和旧坐标回归校验；地图包必须一次性通过全部
   11 张 SVG、272 张瓦片与 1 张灯塔 2D 参考图的格式、尺寸和楼层校验；
4. 下载只写入临时目录，完整校验后以哈希版本原子激活；页面通过版本号自动刷新任务目录和地图缓存；
5. 内容更新失败时依次使用当前 `mapapp-content.json`、内置内容快照；地图更新失败时依次使用
   `mapapp-map-cache` 当前版本、程序内嵌地图。失败包不会覆盖可用版本。

任务、撤离点和地图来源对象 ID 会根据上游 `normalizedName` 动态归入现有地图，因此普通数据更新及
上游对象 ID 调整不再需要修改代码。若未来出现全新地图或全新渲染格式，仍需先确认底图授权和投影方式，
程序会忽略无法安全展示的坐标，而不是将其错误投影到其他地图。

## 校准（重要）

默认投影参数（边界、旋转、镜像）只是起点，**必须实测校准**（PLAN §4）：

1. 游戏里走到一个容易辨认的位置，按截图键；
2. 网页面板 → 开启「校准模式」→ 点击地图上你实际所在的位置；
3. 在 3 个以上（建议 5–10 个）不共线位置重复；
4. 点「拟合」保存仿射变换，网页显示 RMS 像素误差。

坐标轴顺序（`axisOrder`）默认为 `x,y,z`，如实测不符可改 `internal/registry/maps.json` 后重新构建。
楼层自动切换需在面板中配置每层 worldY 范围。

## 地图素材授权

11 张地图 SVG 为 the-hideout/tarkov-dev-svg-maps 的 CC BY-NC-SA 4.0 素材。迷宫和破冰船底图来自
tarkov.dev 当前交互地图瓦片，登记作者分别为 Tarkov.dev 与 TarkovBOT.eu；灯塔另内置 Tarkov.dev 当前
2242×3892 的 2D 参考图，仅在交互 SVG 不可用时显示。由于参考图未与世界坐标重新标定，兜底模式会暂停
玩家、任务和撤离点标记，只保留单指拖动、双指缩放和按钮缩放。本项目按既定的个人、非商业、本地使用范围
固定打包并显示署名，**运行时完全不需要联网**。瓦片和静态图并不自动继承 SVG 仓库的许可，发布、分发
或商用前必须另行确认作者授权。具体来源见 `THIRD_PARTY_NOTICES.md` 和
`docs/research/2026-09-21-map-data-update.md`。

## API

| 端点 | 说明 |
| --- | --- |
| `GET /api/state` | 当前地图、位置、历史、地图注册表 |
| `GET /api/events` | SSE 实时推送 |
| `POST /api/ingest/screenshot` | `{filename}` 解析截图文件名 |
| `POST /api/ingest/position` | `{mapId?, world:{x,y,z}, rotation?}` 直接注入坐标 |
| `POST /api/ingest/map` | `{map}` 原始日志名或地图 ID，未知预设不会误切 |
| `POST /api/ingest/quest` | `{questId, status}` 上报任务状态（questId 为 BSG gameId） |
| `POST /api/quests/{id}/status` | 手动设置任务状态（空 status 清除） |
| `GET /api/quests` | 任务目标数据（静态） |
| `GET /api/catalog/meta` | 活动内容包版本、来源和各地图完整度 |
| `GET /api/tasks?mode=pvp\|pve&mapId=` | 任务图谱，可按地图过滤 |
| `GET /api/maps/{id}/features?mode=` | 撤离点、转场点及本图任务 |
| `GET /api/progress?profile=&mode=&wipe=` | 指定档案任务与目标状态 |
| `PUT /api/progress/scope` | 切换当前档案、模式和删档周期 |
| `PUT /api/progress/tasks/{id}` | 手动设置整项任务状态 |
| `PUT /api/progress/objectives/{id}` | 手动设置单个目标状态 |
| `POST /api/ingest/quest-events` | agent 幂等批量上报任务日志事件 |
| `GET /api/log-replay/candidates` | 历史日志补录预览 |
| `POST /api/log-replay` | 确认应用历史日志补录 |
| `POST /api/maps/{id}/projection` | 保存投影/校准配置 |
| `POST /api/maps/{id}/floors` | 保存楼层 worldY 范围 |
