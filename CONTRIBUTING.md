# Contributing to TarkovPilot Atlas

感谢你帮助改进 TarkovPilot Atlas。

## 开始之前

1. 先搜索已有 Issue，避免重复工作。
2. 涉及地图、任务或翻译的数据修正，请附上可核验的公开来源。
3. 不要提交游戏私有接口、内存读取、注入、反作弊绕过或来源不明的受版权保护素材。
4. 不要把账号、令牌、本地日志、游戏档案或 `mapapp-data.json` 提交到仓库。

## 本地验证

```bash
cd mapapp
go test ./...

cd frontend
npm install
npm test
npm run build
```

坐标投影、日志状态机和内容同步变更必须包含回归测试。地图素材变更还应更新
`mapapp/THIRD_PARTY_NOTICES.md`，记录来源、作者、授权和校验信息。

## Pull Request

请在说明中写清：

- 解决的问题和用户可见变化；
- 使用的数据来源及授权；
- 执行过的测试；
- 尚未验证或需要人工确认的边界。
