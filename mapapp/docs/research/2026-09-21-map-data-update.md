# 2026-09-21 塔科夫公开地图数据更新核对

## 1. 调查范围与结论

查询时间：**2026-09-21 15:30（Asia/Shanghai，07:30 UTC）**。

本次只核对以下一手来源，不修改 TarkovPilot 业务代码或活动数据包：

- `json.tarkov.dev` 的 PvP/PvE 地图与任务静态接口；
- `the-hideout/tarkov-dev-svg-maps` 的主分支文件、提交记录与文件哈希；
- `the-hideout/tarkov-dev` 网站源代码中的地图注册表、静态地图补充数据及相关提交。

结论摘要：

1. `json.tarkov.dev` 当前返回 **17 个地图记录**，比 TarkovPilot 当前登记的 13 个上游记录（合并后 11 张底图）多出 4 个记录：**The Labyrinth、Ground Zero Tutorial、Icebreaker、The Lab (Dark)**。
2. 新记录中只有 **The Labyrinth** 公开了撤离点（2 个）；四个新记录均没有自身转场点。现有 Lighthouse、Shoreline 各新增一个 `Transit to Icebreaker`，转场 ID 都是 `51`。
3. 任务静态数据已经包含 Labyrinth、Icebreaker、Dark Lab 的地图关联和部分世界坐标；Ground Zero Tutorial 只有任务地图关联，没有可投影坐标；Terminal 仍没有普通任务坐标。
4. `tarkov-dev-svg-maps` 当前仍只有项目已经内嵌的 **11 个 SVG**。项目中的 11 个文件与仓库主分支逐文件 SHA-256 完全一致，因此 **SVG 仓库本身没有需要更新的资产**。
5. tarkov.dev 网站已经为 Labyrinth 和 Icebreaker 配置了交互式 PNG 瓦片；Dark Lab 只是 The Lab 的 `altMaps` 变体；Ground Zero Tutorial 没有独立地图视图。它们不能被当成新的 SVG 文件直接接入。
6. 上游撤离点 `id` 不能视为长期稳定 ID：与项目 2026-08-29 快照对比，旧有 145 个撤离点 ID 仅 4 个仍相同；当前数据还存在 5 组“同一地图、同一 ID、不同阵营或坐标”的记录。现有按 `mapId + kind + rawId` 去重的逻辑会丢标记，接入新数据前必须先修正身份和去重模型。

## 2. 可复核的数据版本

### 2.1 json.tarkov.dev

| 数据 | URL | Last-Modified | ETag | 下载字节 | SHA-256 | 数量 |
|---|---|---|---|---:|---|---|
| PvP 地图 | [regular/maps](https://json.tarkov.dev/regular/maps) | 2026-09-21 06:54:26 UTC | `5d83b62f54b3537337236ecb392db73a` | 8,542,292 | `905ebbbfc657b405f431ea901f9d9ca606e0269df4b7d5a19faf75e138c45512` | 17 地图记录、152 条原始撤离、33 条原始转场 |
| PvE 地图 | [pve/maps](https://json.tarkov.dev/pve/maps) | 2026-09-21 06:54:32 UTC | `f82f58d5c94e820de25c65c4477ad332` | 8,569,440 | `9730c19053c5797882ddaf4c041c4067baee01b775a6958ca344ce2a152d1707` | 17 地图记录；撤离/转场数量与 PvP 相同 |
| PvP 任务 | [regular/tasks](https://json.tarkov.dev/regular/tasks) | 2026-09-21 00:53:18 UTC | `9a2515563782a94a1c625c7a85d07c43` | 2,131,443 | `83f3b27deb0c9b53dc89e5e1e4981ad77d0dd2484bc42b356f43e517842f8f6b` | 515 任务、1,441 目标、608 区域、320 个可能位置点 |
| PvE 任务 | [pve/tasks](https://json.tarkov.dev/pve/tasks) | 2026-09-21 00:53:25 UTC | `a60acbfe3b6f742823e71c55b3b6cce0` | 2,065,082 | `fe42d3810e52ad29ca89e5e1e4981ad77d0dd2484bc42b356f43e517842f8f6b` | 512 任务、1,418 目标、608 区域、329 个可能位置点 |

说明：表中的“原始撤离/转场”是接口数组长度之和，尚未合并 Factory 日夜、Ground Zero 普通/21+，也未处理重复 ID。

### 2.2 源代码仓库

| 仓库/文件 | 核对版本 | 证据 |
|---|---|---|
| SVG 地图仓库 | `5a8b6115d1c0cf56f2ebaac1a96fa5ae3074d178`，2026-02-16，`Adding Terminal` | [固定提交](https://github.com/the-hideout/tarkov-dev-svg-maps/commit/5a8b6115d1c0cf56f2ebaac1a96fa5ae3074d178)、[仓库](https://github.com/the-hideout/tarkov-dev-svg-maps) |
| tarkov.dev 地图注册表 | 仓库 HEAD `7ecdddf3a29d753fad14a461ca4c00921b620887`；地图文件最近功能提交为 2026-07-20 | [固定版本 maps.json](https://github.com/the-hideout/tarkov-dev/blob/7ecdddf3a29d753fad14a461ca4c00921b620887/src/data/maps.json) |
| 地图静态补充数据 | 同上；当前只含 Terminal 的 4 个狙击 Scav 出生坐标 | [固定版本 maps_static.json](https://github.com/the-hideout/tarkov-dev/blob/7ecdddf3a29d753fad14a461ca4c00921b620887/src/data/maps_static.json) |

与新增地图直接相关的提交：

- Labyrinth 投影修正：[`65ed2de`](https://github.com/the-hideout/tarkov-dev/commit/65ed2de2057dca1e7cec1c27572e28aad26a6386)，2026-06-05。
- Dark Lab 作为 The Lab 的替代地图：[`d52c534`](https://github.com/the-hideout/tarkov-dev/commit/d52c5345b1042a8baaa888919ad7f4cbd11e27dc)，2026-07-14。
- Icebreaker 交互地图：[`9fd29f7`](https://github.com/the-hideout/tarkov-dev/commit/9fd29f7f16e5d5408831d9be48274dcb8862de1d)，2026-07-20。

## 3. 当前公开地图记录与稳定地图 ID

下表来自 2026-09-21 的 `regular/maps`；PvE 返回同一组地图记录及相同撤离/转场数量。

| 地图 | normalizedName | 上游地图 ID | 撤离 | 转场 | TarkovPilot 当前状态 |
|---|---|---|---:|---:|---|
| Factory | `factory` | `55f2d3fd4bdc2d5f408b4567` | 10 | 3 | 已登记，合并为 `factory` |
| Night Factory | `night-factory` | `59fc81d786f774390775787e` | 9 | 3 | 已登记，合并为 `factory` |
| Customs | `customs` | `56f40101d2720b2a4d8b45d6` | 27 | 4 | 已登记 |
| Woods | `woods` | `5704e3c2d2720bac5b8b4567` | 19 | 4 | 已登记 |
| Lighthouse | `lighthouse` | `5704e4dad2720bb55b8b4567` | 8 | 4 | 已登记 |
| Shoreline | `shoreline` | `5704e554d2720bac5b8b456e` | 18 | 4 | 已登记 |
| Reserve | `reserve` | `5704e5fad2720bc05b8b4567` | 10 | 3 | 已登记 |
| Interchange | `interchange` | `5714dbc024597771384a510d` | 9 | 2 | 已登记 |
| Streets of Tarkov | `streets-of-tarkov` | `5714dc692459777137212e12` | 19 | 3 | 已登记 |
| The Lab | `the-lab` | `5b0fc42d86f7744a585f9105` | 6 | 1 | 已登记为 `lab` |
| Ground Zero | `ground-zero` | `653e6760052c01c1c805532f` | 9 | 1 | 已登记，合并为 `groundzero` |
| Ground Zero 21+ | `ground-zero-21` | `65b8d6f5cdde2479cb2a3125` | 6 | 1 | 已登记，合并为 `groundzero` |
| Terminal | `terminal` | `65cc8f81a9aac3e77d0cfd3e` | 0 | 0 | 已登记，但公开数据仍缺失 |
| **The Labyrinth** | `the-labyrinth` | `6733700029c367a3d40b02af` | **2** | 0 | **未登记** |
| **Ground Zero Tutorial** | `ground-zero-tutorial` | `68236e8153654e8c1200798a` | 0 | 0 | **未登记** |
| **Icebreaker** | `icebreaker` | `69af492a4819ea4ba10a69c5` | 0 | 0 | **未登记** |
| **The Lab (Dark)** | `the-lab-dark` | `6a294a5b5eb5f9a1700417b7` | 0 | 0 | **未登记** |

这些 24 位地图 ID 是游戏地图对象 ID，适合保留为 `sourceMapId`。内部展示 ID 仍应由项目控制；Dark Lab、Ground Zero Tutorial 不应直接等同于独立底图。

## 4. 新地图的数据完整度

任务坐标统计口径：`zones` 中每个区域计 1 个坐标区域，`possibleLocations[].positions` 中每个位置计 1 个点；“关联任务/目标”表示目标声明了该地图，但不一定带坐标。

| 地图记录 | 关联任务/目标（PvP） | 有坐标的任务/目标 | PvP 坐标 | PvE 坐标 | 撤离/转场 | 地图素材结论 |
|---|---:|---:|---:|---:|---:|---|
| Labyrinth | 9 / 14 | 6 / 10 | 8 区域 + 10 点 | 8 区域 + 10 点 | 2 / 0 | SVG 仓库无文件；tarkov.dev 有 `maps/labyrinth/main` PNG 瓦片和一个 2D 视图描述 |
| Ground Zero Tutorial | 11 / 11 | 0 / 0 | 0 | 0 | 0 / 0 | 无独立 SVG、瓦片或视图注册；应视为 Ground Zero 教程变体元数据 |
| Icebreaker | 16 / 19 | 1 / 1 | 3 点 | 6 点 | 0 / 0 | SVG 仓库无文件；tarkov.dev 有 16 层交互 PNG 瓦片和一个 2D 视图描述 |
| The Lab (Dark) | 14 / 17 | 3 / 3 | 10 点 | 10 点 | 0 / 0 | 无独立 SVG/瓦片；网站通过 `altMaps: ["the-lab-dark"]` 复用 The Lab 投影和素材 |
| Terminal | 0 / 0 | 0 / 0 | 0 | 0 | 0 / 0 | 有 `Terminal.svg`；普通地图/任务接口仍没有可显示要素，`maps_static.json` 只有 4 个狙击 Scav 出生坐标 |

Labyrinth 的两个撤离点为：

- `labir_exit`，PMC，ID `8eabbd6a5b3d8d277b4acdbfc2ba735ff5a26ae8`；
- `labyrinth_secret_tagilla_key`，PMC，ID `8fd7a20a588f2243a186af931a5c0224ae01e972`，公开数据带转移物品条件。

Icebreaker 虽然自身没有转场记录，但现有地图出现两个入口：

- Lighthouse：转场 ID `51`，`Transit to Icebreaker`，位置 `(147.37, 0.30, -182.55)`；
- Shoreline：转场 ID `51`，`Transit to Icebreaker`，位置 `(-315.70, -63.66, 525.71)`。

因此转场 ID 是**地图作用域内 ID**，不能把 `51` 当作全局唯一键。

## 5. 现有 11 张地图的变化

项目内置快照版本为 `20260829T163110Z-7b34844d0203`，生成于 2026-08-29 16:31:10 UTC，包含 PvP 517/PvE 514 个任务、每种模式 172 个归一化地图要素。下表对比该快照与 2026-09-21 公开数据。

“当前归一化”按项目现有 `mapId + kind + rawId` 合并规则模拟；它不是可靠业务数量，因为当前上游已经出现重复 raw ID。

| 内部地图 | 旧快照撤离/转场 | 当前原始撤离/转场 | 按现逻辑归一化 | 旧→新有坐标任务（PvP） | 主要变化 |
|---|---:|---:|---:|---:|---|
| factory | 18 / 3 | 19 / 6（日夜合计） | 17 / 3 | 22 → 22 | Gate 2 出现；日夜与阵营记录发生 ID 合并冲突 |
| customs | 27 / 4 | 27 / 4 | 27 / 4 | 41 → 44 | 数量不变，但撤离 ID 全面刷新 |
| woods | 20 / 4 | 19 / 4 | 17 / 4 | 34 → 37 | Friendship Bridge 消失；Outskirts、UN Roadblock 出现同 ID 多阵营记录 |
| shoreline | 14 / 3 | 18 / 4 | 18 / 4 | 39 → 42 | 新增 CCP Temporary、Cliff Descent、Rock Passage、Ruined House Fence、Svetliy Dead End；Railway Bridge 消失；新增前往 Icebreaker 转场 |
| interchange | 6 / 2 | 9 / 2 | 8 / 2 | 13 → 16 | 新增 Path to River、Smugglers' Tunnel 等；NW Exfil 出现同 ID 多阵营记录 |
| lab | 7 / 1 | 6 / 1 | 6 / 1 | 5 → 6 | Medical Block Elevator 不再返回；Dark Lab 是另一个上游地图记录 |
| reserve | 11 / 3 | 10 / 3 | 10 / 3 | 24 → 27 | D-2 不再返回 |
| lighthouse | 13 / 3 | 8 / 4 | 8 / 4 | 30 → 34 | 5 个旧撤离不再返回；新增前往 Icebreaker 转场 |
| streetsoftarkov | 17 / 3 | 19 / 3 | 19 / 3 | 43 → 44 | 新增 Basement Entrance、Scav Checkpoint |
| groundzero | 12 / 1 | 15 / 2（普通/21+ 合计） | 15 / 1 | 11 → 11 | 新增 Pinewood Basement、Scav Bunker、UN Roadblock；普通/21+ 转场合并为 1 条 |
| terminal | 0 / 0 | 0 / 0 | 0 / 0 | 0 → 0 | 数据缺口未改善 |

需要注意：上表的“消失”只表示 2026-09-21 静态接口不再返回，不等同于已证明游戏内永久移除。接入时应保留版本报告和人工复核，而不是直接将旧标记永久删除。

## 6. ID 稳定性和当前去重风险

### 6.1 实测稳定性

- 地图 ID：项目已登记的 13 个上游地图记录 ID 均仍存在；4 个未登记 ID 可作为新增来源 ID。
- 任务 ID：PvP 快照 517 个任务中，当前保留 515 个；移除了 `Forklift Certified`（`669fa394e0c9f9fafa082897`）和 `Capacity Check`（`669fa3a1c26f13bd04030f37`），没有新增任务 ID。任务/目标 ID 仍适合作为版本间主匹配键，但删除必须进入变更报告。
- 撤离点 ID：旧快照 145 个撤离点与当前 145 个归一化撤离点中，只有 **4 个 ID 相同**。这说明撤离点 ID 没有可依赖的长期稳定性契约。
- 转场 ID：旧有 27 个 ID 全部保留，新增 2 条 ID 都是 `51`；它只能在来源地图内唯一。

### 6.2 当前接口中的重复撤离 ID

当前 PvP 地图数据有 5 组同一来源地图内的重复撤离 ID：

1. Factory：`f16ce...` 同时表示 PMC/Scav 的 Gate 3，坐标不同；
2. Night Factory：`51ab65...` 对应两条 Gate 3，坐标不同且 faction 为空；
3. Woods：`5e2529...` 同时表示 PMC/Scav Outskirts；
4. Woods：`bd8c86...` 同时表示 PMC/Scav UN Roadblock；
5. Interchange：`7954d8...` 同时表示 PMC/Scav NW Exfil。

项目当前 [`internal/content/sync.go`](../../internal/content/sync.go) 的 `mergeFeature()` 以 `mapId + kind + rawId` 去重，会静默丢掉上述每组的后一条记录。不能直接执行数据刷新并将结果视为完整。

建议身份模型：

- `sourceMapId` 使用上游 24 位地图 ID；
- `rawFeatureId` 原样保留，但标记为“上游版本 ID”，不再作为唯一主键；
- 转场本地主键至少包含 `sourceMapId + transitId`；
- 撤离点建立项目控制的稳定 ID，并保存版本化别名。首次匹配使用地图变体、原始名称键、阵营和空间邻近关系，无法唯一匹配时必须进入人工校正报告，不做静默合并；
- Factory 日夜、Ground Zero 普通/21+ 只能在确认名称、阵营和位置一致后合并，不能只按 raw ID 合并。

## 7. SVG 与其他地图素材

[`the-hideout/tarkov-dev-svg-maps`](https://github.com/the-hideout/tarkov-dev-svg-maps) 当前主分支只有以下 11 个 SVG：

`Customs.svg`、`Factory.svg`、`GroundZero.svg`、`Interchange.svg`、`Labs.svg`、`Lighthouse.svg`、`Reserve.svg`、`Shoreline.svg`、`StreetsOfTarkov.svg`、`Terminal.svg`、`Woods.svg`。

TarkovPilot `internal/server/maps/` 中的 11 个文件与仓库 HEAD 逐文件 SHA-256 完全一致。仓库最近提交仍是 2026-02-16 的 Terminal，因此本轮不存在 SVG 更新。

tarkov.dev 网站的 [`src/data/maps.json`](https://github.com/the-hideout/tarkov-dev/blob/7ecdddf3a29d753fad14a461ca4c00921b620887/src/data/maps.json) 另有：

- Labyrinth：`https://assets.tarkov.dev/maps/labyrinth/main/{z}/{x}/{y}.png`；
- Icebreaker：16 层 `https://assets.tarkov.dev/maps/icebreaker/.../{z}/{x}/{y}.png`；
- Dark Lab：通过 `altMaps` 复用 The Lab；
- Ground Zero Tutorial：没有独立视图。

这些 PNG 瓦片不属于 `tarkov-dev-svg-maps` 的 11 个 SVG 文件。SVG 仓库的 CC BY-NC-SA 4.0 许可不能自动推定覆盖瓦片；在确认作者与再分发条款前，不建议把瓦片打包进可执行文件。

## 8. 建议接入范围

### 必须先做

1. 修复撤离点身份和去重逻辑，增加重复 ID、ID 大面积变更、同名多阵营和坐标漂移报告；否则刷新会丢失至少 5 条当前公开记录。
2. 将现有 11 张地图的数据包刷新作为独立变更审核，特别人工核对 Lighthouse、Shoreline、Woods、Interchange、Ground Zero 的撤离增删。
3. 为 `Transit to Icebreaker` 增加目的地图 ID 解析，但在 Icebreaker 底图未接入前，界面应明确显示“目的地图素材暂缺”，不能错误指向其他地图。

### 推荐纳入本轮

- **Dark Lab**：作为 `lab` 的地图变体接入，不新增独立底图；复用 Labs 投影并保留 `sourceMapId=6a294...`，展示 3 个有坐标任务目标。
- **Ground Zero Tutorial**：登记为 Ground Zero 变体/别名，不新增独立底图；当前只保留 11 个任务目标的地图关联，不生成标记。
- **Terminal**：保留现状和“公开数据暂缺”提示；不要把 4 个狙击 Scav 出生坐标误当撤离点或任务点。

### 需要地图素材方案后再纳入

- **Labyrinth**：数据完整度已经足以展示 2 个撤离点和 10 个有坐标目标，但没有 SVG。若项目坚持离线 SVG，应等待或自行制作可授权 SVG；若改用瓦片，需要新增离线瓦片投影、楼层和授权审查。
- **Icebreaker**：没有自身撤离/转场，仅有 1 个带坐标任务目标，但已有两条入场转场；没有 SVG，且网站交互图为 16 层瓦片。建议先完成多层瓦片底图和许可确认，再开放为可选地图。

### 不建议

- 不建议把 Labyrinth/Icebreaker 的网站瓦片伪装成 SVG 或运行时热链；
- 不建议把 Dark Lab、Ground Zero Tutorial 作为全新物理地图重复注册；
- 不建议继续把上游撤离点 ID 作为长期稳定业务主键；
- 不建议仅依据接口数量变化自动删除旧撤离点，必须保留上一版本并生成可审阅差异。

## 9. 本次未完成的业务动作

- 未修改地图注册表、内容同步器、前端或数据包；
- 未下载/打包 Labyrinth、Icebreaker 的瓦片素材；
- 未执行 `tarkovmap data refresh -apply`；
- 未做游戏内实地坐标校准；
- 未将任何调研结论视为实际部署或业务验收结果。
