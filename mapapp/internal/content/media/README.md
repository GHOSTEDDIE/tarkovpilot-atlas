# 审核任务截图

图片必须保存在本目录，并在 `../data/media.json` 中登记。`objectiveId` 使用内容包里的
`progressId`（`taskId:objectiveId`），避免上游复用目标 ID 时串图。

```json
{
  "objectiveId": "taskId:objectiveId",
  "mapId": "customs",
  "localFile": "customs/example.webp",
  "captionZh": "入口视角",
  "sourceUrl": "https://example.com/source",
  "author": "作者或项目名",
  "license": "明确的授权名称",
  "gameVersion": "游戏版本",
  "checksum": "sha256:...",
  "reviewedAt": "2026-08-30T00:00:00Z"
}
```

构建和启动时会校验目标引用、地图、文件路径、SHA-256 和图片尺寸。未登记或校验失败的图片不会展示。
