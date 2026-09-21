# 2026-09-21 灯塔底图更新核对

## 结论

TarkovPilot 在本轮更新中接入了灯塔的最新任务、撤离点和转场数据，并把最新竖版 2D 图作为
**离线静态兜底**内置；正常定位仍使用可投影的交互 SVG。

项目当前仍使用可叠加世界坐标的 `internal/server/maps/Lighthouse.svg`。截至 2026-09-21：

- [Tarkov.dev 地图注册表](https://github.com/the-hideout/tarkov-dev/blob/7ecdddf3a29d753fad14a461ca4c00921b620887/src/data/maps.json)中的灯塔交互地图仍指向 `https://assets.tarkov.dev/maps/svg/Lighthouse.svg`，没有配置 PNG 瓦片；
- [SVG 地图仓库](https://github.com/the-hideout/tarkov-dev-svg-maps/tree/5a8b6115d1c0cf56f2ebaac1a96fa5ae3074d178)主分支仍是 `5a8b611...`，项目文件与该仓库的 `Lighthouse.svg` SHA-256 都是 `c1b2603ac306e66900c0db8b68807554e7c6230c031142a3541fc7c065f8d013`；
- 从 Tarkov.dev 资源域下载的当前 SVG SHA-256 为 `67fcba39a45e896943e5b0befd0d8d2cf54a88334e8c24136f8e0c0abe31713e`，格式化比较后唯一结构变化是 `Ground_Level` 多了 `data-layer="Ground_Level"`，路径几何没有变化。因此替换它不会得到视觉上的“新灯塔图”。

## Tarkov.dev 的其他灯塔图片

Tarkov.dev 还登记了三个静态视图，但它们不属于交互坐标底图：

| 视图 | 文件 | 尺寸 | 最近相关提交 |
|---|---|---:|---|
| 竖版 2D | `public/maps/lighthouse-2d.jpg` | 2242×3892 | `759bded...`，2026-01-08，`Updated all the other 2D maps` |
| 横版 2D | `public/maps/lighthouse-2d-landscape.jpg` | 3498×2111 | 最近视觉更新 `a10b06f...`，2023-06-30；2023-11-14 仅重新压缩 |
| 3D | `public/maps/lighthouse-3d.jpg` | 8259×7560 | `756ec500...`，2026-01-07，`Updated all re3mr maps...` |

这些图片在 Tarkov.dev 页面中通过普通 `<img>` 查看；只有 `projection: "interactive"` 的地图进入 Leaflet 坐标、撤离点和任务标记管线。它们没有与 TarkovPilot 当前 SVG 投影等价的坐标映射，不能直接替换后继续宣称任务点位置准确。

本次已将竖版 2D 图 `2242×3892`、SHA-256
`e558e75aea610ac5d54e6d8c5219067be0c6666dbbde4dee4b1b2ef14d6cdd26` 固定打包，并登记
`https://tarkov.dev/maps/lighthouse-2d.jpg` 为自动更新来源。交互 SVG 请求失败或返回无效格式时，页面会切换到
该本地参考图，保留拖动和缩放，但暂停玩家、任务、撤离点与转场标记。

## 本轮灯塔数据实际变化

最新 `json.tarkov.dev` 数据已经进入 TarkovPilot：

- 灯塔公开撤离点由旧快照的 13 个变为 8 个；
- 转场由 3 个变为 4 个，新增 `Transit to Icebreaker`；
- PvP 有坐标任务由 30 个变为 34 个；
- 玩家、任务、撤离点和转场仍投影到原 `Lighthouse.svg`。

数据来源：[PvP maps](https://json.tarkov.dev/regular/maps)、[PvP tasks](https://json.tarkov.dev/regular/tasks)。

## 后续接入建议

若未来希望把 2D 图升级为可定位底图，应完成至少 3–5 个控制点的仿射校准和误差验收后再开放玩家、任务和
撤离标记。若指的是游戏近期地形改版，则当前一手上游尚未提供新版可投影 SVG 或灯塔瓦片，需要另找具有
明确授权和坐标基准的底图。
