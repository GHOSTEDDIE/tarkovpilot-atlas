# 塔科夫地图坐标定位 App：调研结论与实施计划

> 文档状态：调研与方案阶段，尚未开始实现。
>
> 调研日期：2026-08-19

## 1. 目标

开发一个独立的 Windows 地图定位 App，整体思路沿用 TarkovPilot：

```text
EFT 生成游戏截图
  -> 监听 Screenshots 文件夹
  -> 解析截图文件名中的世界坐标与镜头旋转
  -> 监听游戏日志识别当前地图
  -> 将世界坐标转换为地图像素坐标
  -> 在独立地图窗口中显示玩家位置
```

第一阶段只读取截图文件名和游戏日志，不读取游戏进程内存，不注入游戏，不修改游戏文件，不自动操作鼠标键盘。

## 2. 已确认的信息

### 2.1 截图文件名包含定位数据

公开项目给出的截图文件名示例：

```text
2025-12-20[02-09]-420.18, 1.00, 319.01-0.00089, -0.99307, -0.00012, -0.11748_15.11 (0).png
```

其中包含：

- 日期和时间；
- 三个世界坐标值；
- 四元数形式的相机旋转值；
- 其他截图序号/版本信息。

参考：[TanukiTarkovMap 项目说明](https://github.com/siakun/TanukiTarkovMap)。

当前项目的历史 C# 版本曾经用正则解析截图文件名并发送 `POSITION_UPDATE`。后来在提交 `5d6d639` 中改成只发送完整文件名，由远端解析坐标。当前 Go 版本继续使用“监听文件名”的方式：

- [截图监听](app/internal/watcher/screens.go)
- [截图事件处理](app/app.go)
- [远端 webhook 客户端](app/internal/api/client.go)

### 2.2 坐标轴顺序必须通过实测确认

现有资料对坐标顺序的标注并不完全一致：

- 当前项目历史正则按 `y,z,x` 读取文件名中的三个数字；
- 新的公开项目文档将示例标记为 `x,y,z`。

因此不能直接把任一项目的解析器当作绝对协议。实现时要保留 `axisOrder` 配置，并通过已知地图位置进行校准。

建议内部统一保存为：

```json
{
  "x": 0.0,
  "y": 0.0,
  "z": 0.0,
  "rotation": {
    "x": 0.0,
    "y": 0.0,
    "z": 0.0,
    "w": 0.0
  },
  "rawFilename": "..."
}
```

解析器输出统一字段，坐标轴顺序只在解析器配置中处理，不让 UI 和地图投影层感知文件名差异。

### 2.3 当前项目已经有可复用的日志监听逻辑

当前项目从 EFT 日志中识别地图：

- PVP：`TRACE-NetworkGameCreate profileStatus` 行中的 `location: <name>,`；
- PVE：`scene preset` 行中的 `path:maps/<name>.bundle`；
- 只读取最新会话目录和增量日志内容。

参考：[app/internal/watcher/logs.go](app/internal/watcher/logs.go)。

建议将日志字符串解析和地图别名映射保留在独立模块中，输出统一的 `MapId`，不要让 UI 直接依赖原始日志字符串。

典型映射包括：

| 日志名称 | 内部地图 ID |
| --- | --- |
| `bigmap` | `customs` |
| `factory4_day` | `factory` |
| `factory4_night` | `factory` |
| `RezervBase` | `reserve` |
| `laboratory` | `lab` |
| `TarkovStreets` | `streetsoftarkov` |
| `Sandbox` / `Sandbox_high` | `groundzero` |

参考：[SPT 地点名称映射](https://github.com/sp-tarkov/wiki/blob/main/modding/references/location-information.md)。

地图名称和地图预设是一对多关系。例如 Factory 有昼夜预设，Ground Zero 可能按等级区分预设。应把预设映射表设计成可更新配置，而不是写死在 UI 中。

### 2.4 公开地图数据可作为初始数据源

可参考以下数据源：

- [tarkov.dev](https://tarkov.dev/)：公开的 EFT 社区 API；
- [TarkovTracker/tarkovdata](https://github.com/TarkovTracker/tarkovdata)：地图元数据和任务数据；
- [tarkov-dev-svg-maps](https://github.com/the-hideout/tarkov-dev-svg-maps)：SVG 地图素材；
- [the-hideout/tarkov-api](https://github.com/the-hideout/tarkov-api)：GraphQL API 和 `MapPosition` 数据结构。

`maps.json` 中可以看到地图的以下信息：

- SVG 文件名；
- 楼层列表；
- 默认楼层；
- `coordinateRotation`；
- 地图坐标边界 `bounds`。

参考：[maps.json](https://raw.githubusercontent.com/TarkovTracker/tarkovdata/master/maps.json)。

注意地图资料会随游戏版本变化，地图注册表必须支持更新，不能假定旧地图数据永久有效。

## 3. 推荐技术方案

### 3.1 本地端到端方案

推荐第一版直接在本地完成解析和展示：

```text
ScreenshotWatcher
  -> ScreenshotFilenameParser
  -> PositionEvent
  -> MapSessionResolver
  -> CoordinateProjection
  -> MapRenderer
```

模块职责：

| 模块 | 职责 |
| --- | --- |
| `ScreenshotWatcher` | 监听截图目录，过滤 EFT 游戏截图，去重文件事件 |
| `ScreenshotFilenameParser` | 解析日期、坐标、四元数、原始文件名 |
| `LogsWatcher` | 读取最新会话目录，识别地图预设 |
| `MapSessionResolver` | 将日志预设映射为内部 `MapId`，维护当前地图 |
| `CoordinateProjection` | 将世界坐标转换为地图像素坐标 |
| `MapRepository` | 保存地图图片、SVG、边界、旋转、楼层配置 |
| `MapRenderer` | 显示地图、玩家点、朝向、楼层和缩放 |
| `SessionStore` | 保存最近坐标、当前地图、诊断日志 |

单机版本不需要后端，也不需要依赖 tarkov-market 的私有协议。

### 3.2 可选远程同步方案

如果后续需要手机、网页或局域网设备同步，再增加自己的服务：

```text
本地 App
  -> HTTPS/WebSocket
自有后端
  -> Web 页面/移动端
```

事件建议包含：

```json
{
  "event": "position",
  "mapId": "customs",
  "world": { "x": 0.0, "y": 0.0, "z": 0.0 },
  "rotation": { "x": 0.0, "y": 0.0, "z": 0.0, "w": 1.0 },
  "pixel": { "x": 0.0, "y": 0.0 },
  "timestamp": "2026-08-19T00:00:00Z",
  "parserVersion": "1"
}
```

远端只接收结构化坐标，不让远端再次猜测文件名格式。原始文件名只保存在本地诊断日志中，避免不必要地上传完整路径或其他本地信息。

## 4. 坐标投影方案

### 4.1 初始投影

对每张地图保存二维世界边界：

```json
{
  "minX": -371,
  "maxX": 698,
  "minZ": -307,
  "maxZ": 237,
  "rotation": 180
}
```

初始计算过程：

```text
1. 根据实测结果选出水平面坐标，例如 (worldX, worldZ)
2. 按地图 rotation 进行二维旋转
3. 将旋转后的坐标归一化到 [0, 1]
4. 乘以地图图片宽高得到像素坐标
5. 根据地图图片的 Y 轴方向决定是否翻转 pixelY
```

可表达为：

```text
p' = R(rotation) * (p - center) + center
pixelX = (p'.x - minX) / (maxX - minX) * imageWidth
pixelY = (maxZ - p'.z) / (maxZ - minZ) * imageHeight
```

这只是起始模型。最终必须通过实测点校准，因为地图图片的裁剪、旋转、坐标轴方向和不同楼层可能不完全一致。

### 4.2 校准流程

每张地图至少采集 3 个不共线控制点，建议采集 5～10 个：

1. 选择地图上容易确认的位置；
2. 在游戏中移动到该位置并使用游戏自身 Screenshot 键截图；
3. 记录文件名中的坐标；
4. 在地图图片上记录对应像素；
5. 分别测试坐标轴顺序、镜像方向和旋转角度；
6. 用仿射变换或旋转+缩放+平移拟合；
7. 用未参与拟合的点计算误差。

建议每张地图保存：

```json
{
  "axisOrder": "x,y,z",
  "horizontalAxes": ["x", "z"],
  "mirrorX": false,
  "mirrorY": true,
  "rotation": 180,
  "affine": {
    "a": 1.0,
    "b": 0.0,
    "c": 0.0,
    "d": 0.0,
    "e": 1.0,
    "f": 0.0
  },
  "calibrationErrorPixels": 0.0
}
```

### 4.3 楼层判断

`worldY` 不直接作为二维地图坐标，而用于判断楼层：

```text
worldY -> floor range -> selected map layer
```

Factory、Interchange、Labs、Reserve、Streets、Ground Zero 等地图需要独立处理楼层或地下区域。楼层范围应配置化，不能只用一个全局阈值。

### 4.4 朝向显示

截图文件名中的四个旋转值是四元数。第一版可以只显示位置；第二版再完成：

```text
quaternion -> forward vector -> 投影到地图水平面 -> 地图方向箭头
```

朝向同样需要通过“面向已知方向”的实测确定轴和正负号。

## 5. 截图监听注意事项

- 必须使用 EFT 游戏自身的 Screenshot 快捷键；Steam `F12` 截图可能不包含游戏坐标；
- 监听默认目录 `Documents\\Escape from Tarkov\\Screenshots`，同时允许用户手动配置；
- 监听 `Create` 事件并按文件名去重；
- 处理同名事件重复、文件快速生成、游戏退出和目录不存在等情况；
- 只传递文件名，不读取图片内容；
- 进入新地图时清理旧会话截图时应提供开关，避免误删用户文件。

参考：[TanukiTarkovMap 使用说明](https://github.com/siakun/TanukiTarkovMap#使用法)。

## 6. 地图素材和授权风险

`tarkov-dev-svg-maps` 的地图采用 CC BY-NC-SA 4.0，并明确禁止用于促进作弊或获得不公平优势的软件。[许可证](https://github.com/the-hideout/tarkov-dev-svg-maps#-license--use-restrictions)

因此需要在项目立项时确认：

1. 仅个人本地使用，还是要公开发布；
2. 是否需要商业化；
3. 是否直接打包第三方 SVG；
4. 是否需要自己制作地图素材或取得授权；
5. 是否仅调用公开 API，而不复制地图原始素材。

如果无法确认授权，优先做坐标解析和投影验证，不要先把第三方地图素材提交进项目。

## 7. 反作弊和安全边界

实现中明确禁止：

- 读取 EFT 游戏进程内存；
- DLL 注入、图形 API Hook、驱动或内核组件；
- 修改游戏文件；
- 自动化鼠标键盘或游戏操作；
- 读取其他玩家的隐藏状态；
- 将地图工具做成雷达或 ESP；
- 在游戏画面内注入渲染内容。

首版使用独立窗口。若后续增加 Always-on-Top 或透明悬浮窗，必须单独评估风险。BattlEye 官方说明普通非作弊 Overlay 通常不会直接导致封禁，但也不能替代游戏开发者的具体政策。[BattlEye FAQ](https://www.battleye.com/support/faq/)

## 8. 分阶段实施计划

### 阶段 0：坐标格式验证

目标：确认当前游戏版本实际生成的文件名格式和轴顺序。

工作项：

- 收集不同地图、不同高度、不同朝向的游戏截图文件名；
- 验证日期、坐标、四元数和后缀结构；
- 对比当前项目历史正则与其他公开实现；
- 选定内部标准 `PositionEvent`；
- 为异常文件名保留原始值和解析失败原因。

完成标准：能够稳定解析一组真实截图文件名，并明确三个坐标的实际轴含义。

### 阶段 1：本地解析器

目标：不显示地图，只验证坐标解析。

工作项：

- 新建截图文件名解析模块；
- 增加单元测试和真实样本测试；
- 监听目录并输出结构化诊断日志；
- 对重复事件、未知格式、负数、浮点数和截断文件名进行测试。

完成标准：新截图产生后 1 秒内输出坐标事件，错误格式不会导致进程退出。

### 阶段 2：日志识别与会话状态

目标：同时得到 `MapId + PositionEvent`。

工作项：

- 复用当前 `LogsWatcher` 的增量读取逻辑；
- 抽离地图预设到 `MapId` 的映射；
- 支持 PVP、PVE 和新地图未知预设日志；
- 增加“手动选择地图”作为自动识别失败时的回退；
- 处理 App 启动晚于游戏进入地图的情况。

完成标准：地图切换后内部会话状态正确，未知地图不会错误切换到其他地图。

### 阶段 3：单张地图投影

目标：先只支持一张地图完成准确定位。

工作项：

- 选择 Customs 或 Woods 作为第一张地图；
- 准备合法可用的地图图片；
- 实现边界、旋转、镜像和仿射校准配置；
- 显示玩家点和原始 `x/y/z`；
- 建立误差统计。

完成标准：独立验证点的投影误差达到预设目标，建议先控制在 10～20 像素以内。

### 阶段 4：多地图、多楼层和朝向

目标：扩展到主要地图。

工作项：

- 增加地图注册表和版本号；
- 增加楼层范围；
- 增加方向箭头；
- 处理 Factory、Labs、Reserve、Streets 等多层地图；
- 增加地图数据更新机制。

### 阶段 5：桌面体验和可选同步

目标：形成可长期使用的 App。

工作项：

- 独立地图窗口；
- 缩放、拖拽、跟随玩家；
- 全局快捷键；
- 可选 Always-on-Top，默认关闭；
- 可选自有 HTTPS/WebSocket 服务；
- 更新、诊断日志和数据导出。

## 9. 验收清单

### 坐标解析

- [ ] 使用游戏自身 Screenshot 键能触发文件事件；
- [ ] 文件名解析出三个坐标；
- [ ] 坐标轴顺序经实测确认；
- [ ] 四元数解析不丢精度；
- [ ] 异常文件名有明确错误原因。

### 地图识别

- [ ] PVP 地图识别；
- [ ] PVE 地图识别；
- [ ] Factory 昼夜预设识别；
- [ ] Ground Zero 等等级预设识别；
- [ ] 未知预设不会误切地图；
- [ ] 可手动选择地图。

### 投影准确性

- [ ] 至少 3 个控制点完成拟合；
- [ ] 至少 2 个独立点完成验证；
- [ ] 记录每张地图的像素误差；
- [ ] 验证镜像、旋转和楼层；
- [ ] 游戏更新后可重新校准。

### 安全边界

- [ ] 不读取游戏内存；
- [ ] 不注入进程；
- [ ] 不修改游戏文件；
- [ ] 不执行游戏自动化操作；
- [ ] 地图窗口与游戏进程分离；
- [ ] 地图素材授权明确。

## 10. 最终建议

第一版不要从“完整地图产品”开始，而应先完成以下闭环：

```text
真实 EFT 截图
  -> 正确解析坐标
  -> Customs 单地图投影
  -> 本地窗口显示一个位置点
```

最关键的技术风险不是文件监听，而是：

1. 实际坐标轴顺序；
2. 世界坐标到地图图片的投影校准；
3. 多楼层地图的 `worldY` 判定；
4. 地图素材授权和发布边界。

这些问题验证清楚后，再扩展方向箭头、悬浮窗、任务点和远程同步。
