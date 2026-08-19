# tarkovmap — 塔科夫地图定位（自托管后端 + 网页）

按照 `../TARKOV_MAP_POSITION_APP_PLAN.md` 实现的独立定位工具。
**不读游戏内存、不注入、不改游戏文件**：只读取 EFT 自己生成的截图文件名和游戏日志。

```
游戏 PC 上的 agent ──HTTP──> 本服务器 ──SSE──> 浏览器/手机网页
（也可用网页里的“测试注入”直接发坐标，不需要 agent）
```

## 快速开始（Windows 双击即用）

下载/编译 `tarkovmap-windows-amd64.exe`（或 arm64），**双击运行**即可：

- 自动启动服务器 + 内置 agent（实时监听 `%USERPROFILE%\Documents\Escape from Tarkov\Screenshots`，
  自动检测常见安装路径下的游戏日志目录）；
- 自动用默认浏览器打开地图页面；
- 数据（校准、任务状态）保存在 exe 同目录的 `mapapp-data.json`；
- 手机使用：与电脑同一局域网，浏览器打开启动日志里打印的 `http://<电脑IP>:8400`。

游戏里记得用 **EFT 自带截图键**截图（Steam F12 截图不含坐标）。

## 从源码构建

```bash
cd frontend && npm install && npm run build   # 前端构建到 internal/server/static（被 Go 嵌入）
cd .. && go build -o tarkovmap .              # 单文件程序（含网页）

# Windows 交叉编译
GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o tarkovmap-windows-amd64.exe .
```

技术栈：后端 Go + fsnotify；前端 React 19 + Ant Design 5 + Tailwind CSS 4（Vite 构建）。

## 命令行（一般用不到）

```bash
tarkovmap                          # 双击模式 = 下面的 serve 全默认
tarkovmap serve [-addr :8400] [-data FILE] [-token T] [-svg-base URL] [-no-browser] [-agent=false]
tarkovmap agent -server http://<服务器IP>:8400 [-screenshots DIR] [-logs DIR]   # 游戏和服务器分开两台机器时
tarkovmap parse "<截图文件名>"      # 调试图文件名解析
```

服务器参数：`-data` 状态/校准数据文件（默认 `mapapp-data.json`）；`-token` 摄入接口令牌；
`-svg-base` 地图 SVG 来源（默认运行时从 tarkov-dev-svg-maps 仓库拉取，可指向自建镜像）。

手机使用：手机与服务器同一局域网，浏览器打开 `http://<服务器IP>:8400`。

## 工作方式

1. 游戏内用 **EFT 自带截图键**截图（Steam F12 截图不含坐标）；
2. agent 发现新截图 → 只把**文件名**发给服务器（不读图片内容）；
3. 服务器解析文件名中的世界坐标和相机四元数；
4. agent 同时监听日志识别当前地图（PVP `location:` / PVE `scene preset`，支持晚于进图启动）；
5. 网页实时显示玩家位置、朝向箭头、历史轨迹，支持缩放/拖拽/跟随/楼层切换。

## 任务目标标注

后端内嵌任务目标数据（173 个任务、467 个目标点，覆盖全部 10 张地图）：

- 当前地图上以青色菱形显示任务目标点，点击可查看任务名/商人/目标描述并「标记完成」；
- 面板「任务目标」卡片支持按任务名搜索（中/英/俄）、定位到目标点（自动切楼层）、显示/隐藏已完成；
- 任务完成状态保存在服务器（`mapapp-data.json`）。agent 会自动监听 push-notifications 日志中的
  任务通知（`successMessageText` → 完成，`failMessageText` → 失败，`start/acceptMessageText` → 开始）
  并上报；也可以在网页上手动切换，或由外部工具调 `POST /api/ingest/quest`；
  已完成任务的目标点默认自动隐藏。

数据来源：

- 任务定义与目标点坐标：json.tarkov.dev 静态数据导出（tarkov.dev 官方社区数据的镜像），
  目标点为世界坐标（含高度），与玩家位置共用同一套投影/校准管线；
- 任务名/地图名/目标描述中文：游戏官方中文 locale（经 SPT 数据仓库镜像），按 BSG gameId/conditionId
  匹配（个别新任务尚无官方中文名时回退英文）；
- 更新数据：重新从 json.tarkov.dev 拉取后运行数据脚本即可。

## 校准（重要）

默认投影参数（边界、旋转、镜像）只是起点，**必须实测校准**（PLAN §4）：

1. 游戏里走到一个容易辨认的位置，按截图键；
2. 网页面板 → 开启「校准模式」→ 点击地图上你实际所在的位置；
3. 在 3 个以上（建议 5–10 个）不共线位置重复；
4. 点「拟合」保存仿射变换，网页显示 RMS 像素误差。

坐标轴顺序（`axisOrder`）默认为 `x,y,z`，如实测不符可改 `internal/registry/maps.json` 后重新构建。
楼层自动切换需在面板中配置每层 worldY 范围。

## 地图素材授权

地图 SVG 为 the-hideout/tarkov-dev-svg-maps 的 CC BY-NC-SA 4.0 素材。10 张 SVG（约 1.4MB）已内嵌在
程序中、由本地服务器直接提供，**运行时完全不需要联网**，适合个人本地使用；发布/商用前需自行确认授权
（PLAN §6）。也可用 `-svg-base` 指向其他来源（如更新版本地图的自建镜像）。

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
| `POST /api/maps/{id}/projection` | 保存投影/校准配置 |
| `POST /api/maps/{id}/floors` | 保存楼层 worldY 范围 |
