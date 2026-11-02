结论：已复现并修复本次 `plugin account not found` 的真实原因，修复已提交到 main，真实开发站点已能进入 AWS 官方授权页面。**完整验收仍受阻：尚未完成用户真实 AWS 登录、真实 `whoami` 和真实账户再次登录；不能把本次交付标记为端到端验收完成。**

失败请求不是携带了过期账户 ID 的请求，而是旧 Web 包发出的 `{"action":"aws_profiles"}`，根本没有 `id`。该操作已从后端删除，后端却继续查询 `m.records[""]`，返回了误导性的账户不存在错误。

## 已证实的原因与复现

2026-11-02，在修改源代码前：

1. 复制当时运行的开发 daemon 二进制到本 Worker 的临时目录，设置独立 `MEWLA_STATE_ROOT`、`MEWLA_STATE_DIR`、`-state-dir` 和端口 `19876`，使用该目录内的显式 `tmux -S` socket 启动。未读取或复制线上账户凭证。
2. 用命名 agent-browser 会话配对这个独立 daemon，进入 Extensions → AWS。
3. 浏览器记录到先发送 `list`，随后自动发送 `aws_profiles`，没有账户 ID。页面立即出现与用户相同的 “That didn't go through / plugin account not found”。
4. 页面仍显示旧的 “Profiles on this computer / Paste access keys”。当时 `daemon/webui/dist/index.html.gz` 的修改时间为 10 月 28 日，而源码已切换为 AWS 浏览器 OAuth 登录。
5. 检查开发 runner：此前仅监听 daemon Go 文件，构建时只执行 `go build`，既不重新导出 Web 包，也不监听 app 文件。因此较新的后端一直嵌入旧前端。

这解释了为什么无需完成 AWS 登录即可复现报错，也排除了“这次报错由去重删除 pending ID 引起”的猜测。重新登录跳过 disconnected 记录是另一个真实问题，已单独用回归测试证实，不能据此推断线上八条历史记录全部由同一个原因产生。

## 修复与提交

- `1f01214e` — `Keep the dev web bundle in sync with app sources`
  - 开发 runner 首次构建前导出当前 Web 包。
  - 监听 app 源码、资源、工作区依赖配置、patches 和 Web 构建脚本；这些变化先重新导出，再构建 daemon。
  - 排除依赖目录和生成产物，避免构建循环；导出失败保留当前 daemon。
  - 增加监听范围与导出失败回归测试，并调整独立生命周期测试夹具。
- `1a186832` — `Renew disconnected AWS accounts without duplicating cards`
  - AWS 登录成功并确认身份后，允许复用匹配的 disconnected 账户 ID；明确从某张卡发起时仍验证 AWS 账户一致性。
  - 普通登录优先复用已有有效卡片，避免复活另一张 disconnected 重复记录。
  - 重新连接会启用账户，但不会恢复断开时清除的写权限。
  - 未知/已删除操作在账户查询前返回明确的 reload/update 提示，删除原来变成不可达的默认错误分支；未恢复旧 `aws_profiles` 路径。

已更新 Web 开发和插件工程文档。没有发布或部署 release。

为修复当前运行实例，先重新导出了真实 Web 包，再让正常 Go 源文件改动触发现有开发 daemon 的热重载。没有向用户 live tmux `%0` 发送按键，没有直接停止、发送信号或重启该 daemon。

**运行限制：现有开发 watcher 进程不会自行替换其自身程序。当前页面已通过新导出和正常热重载修复，但新的自动 Web 监听逻辑要到开发 runner 下一次正常启动才生效。本次遵守约束，没有为加载 watcher 新逻辑重启它。**

## 验证证据

全部要求的 gates 已通过：

| 检查 | 结果 |
| --- | --- |
| `cd daemon && go vet ./...` | 通过 |
| `cd daemon && go test ./...` | 通过 |
| `cd app && bun test` | 2280 通过，2 跳过，0 失败 |
| `cd app && bunx tsc --noEmit` | 通过 |
| `./scripts/build-web-ui.sh` | 通过，实际重新导出 Web 包 |
| `git diff --check` | 通过 |

第一次全量 Go 测试发现生命周期测试夹具缺少新要求的导出脚本；补齐夹具后，定向测试和再次全量测试均通过。

新增回归测试使用 AWS Sign-In/MCP 的本地 fixture，覆盖普通连接和指定卡片连接、断开后 ID 复用、启用状态、写权限不恢复、重新连接后的 `whoami`，以及当前卡片优先于历史 disconnected 重复记录。通过 Go overlay 将生产代码换回修复前版本时，重复账户与旧操作错误提示测试都确实失败；修复后通过。这些是模拟服务测试，不是用户真实 AWS 账户的成功证据。

真实开发站点验证：刷新 AWS 页面后显示 “Sign in to AWS”，没有原危险提示；点击后发送 `connect_start` / `connect_status`，打开 AWS 官方 “Connect MCP Client to AWS MCP Server” 页面，原页面进入 “Finish signing in to AWS”。没有输入用户凭证或完成授权。

截图和日志保留在本 Worker 临时目录中的 `aws-connect-evidence/`，作为交付证据；账户状态、二进制及运行临时目录已另行清理。

### 修复前：独立 daemon 完整复现原报错

![修复前：独立 daemon 完整复现原报错](/home/daoleno/.mewla/t/RrrgZV/aws-connect-evidence/repro-before.png)

### 修复后：相同独立状态中的 AWS 页面

![修复后：相同独立状态中的 AWS 页面](/home/daoleno/.mewla/t/RrrgZV/aws-connect-evidence/repro-after.png)

### 真实开发站点：新的登录入口

![真实开发站点：新的登录入口](/home/daoleno/.mewla/t/RrrgZV/aws-connect-evidence/live-sign-in-ready.png)

### 真实开发站点：等待 AWS 授权

![真实开发站点：等待 AWS 授权](/home/daoleno/.mewla/t/RrrgZV/aws-connect-evidence/live-waiting.png)

### 真实 AWS 官方授权页：尚未完成人工登录

![真实 AWS 官方授权页：尚未完成人工登录](/home/daoleno/.mewla/t/RrrgZV/aws-connect-evidence/live-aws-authorization.png)

证据文件：

- [修复前请求记录](/home/daoleno/.mewla/t/RrrgZV/aws-connect-evidence/repro-requests.json)
- [真实站点请求记录](/home/daoleno/.mewla/t/RrrgZV/aws-connect-evidence/live-requests.json)
- [修复前回归测试失败摘要](/home/daoleno/.mewla/t/RrrgZV/aws-connect-evidence/regression-before-summary.txt)
- [最终 Go 测试](/home/daoleno/.mewla/t/RrrgZV/aws-connect-evidence/go-test-final.log)
- [最终 go vet](/home/daoleno/.mewla/t/RrrgZV/aws-connect-evidence/go-vet-final.log)
- [Bun 测试](/home/daoleno/.mewla/t/RrrgZV/aws-connect-evidence/bun-test.log)
- [TypeScript 检查](/home/daoleno/.mewla/t/RrrgZV/aws-connect-evidence/tsc.log)

## 清理与未验证项

- 删除的 AWS 账户：**0 条**。八个 disconnected 记录在新设计中仍可被复用，不能视为死数据。按 ID 比较，验证前后全部账户内容一致，其他服务没有变化。
- 已取消本次测试创建但未完成的 AWS 登录流程，撤销本次创建的测试浏览器配对，关闭两个测试浏览器和独立 tmux server；未触碰其他配对设备。
- 用户 Chrome 工具 `mewla_chrome` 未暴露给本 Worker；`mewla agent-use status` 明确返回需要登录这台电脑的桌面才能使用 Chrome。已两次通过本 turn 的 progress `--attention user_input` 向 Brain 请求协助。
- **待人工接续：**用户在刷新后的真实 AWS 页面登录，返回后确认 Connected；随后运行该账户的 `whoami`，再使用同一张卡的 Sign in again，确认 ID 和账户数量不变。若从另一台设备登录，按页面提示把最终 loopback 回调地址粘回原页面。不要将该地址或授权码写入报告或提交。
- 没有进行 Android/iOS 真机回归；本次未修改 TS/TSX，连接修复位于各端共用的 daemon。
- 不具备“一个真实 Connected 账户且工具已工作”的截图。因此当前状态是**代码修复与可执行验证完成，真实登录验收受阻**，不是任务全部完成。
