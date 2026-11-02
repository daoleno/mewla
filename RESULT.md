结论：已复现并修复本次 `plugin account not found` 的真实原因，修复已提交到 main，真实开发站点已能进入 AWS 官方授权页面。**完整验收仍受阻：尚未完成用户真实 AWS 登录、真实 `whoami` 和真实账户再次登录；不能把本次交付标记为端到端验收完成。**

后续用户遇到 AWS 官方页面的 `Bad Request`，这是第二个故障。已对真实 AWS 做 DCR/授权 URL 对照：旧 client、新 client 和真实 zen 页面生成的 URL 均能进入 IAM 登录表单。当前尚未在用户浏览器中证实第二个故障的根因；AWS 已确认的过期浏览器会话问题是有依据的候选，正在通过 Brain 请求隐私窗口对照。没有把第二个问题误标为已修复。

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

## 后续：AWS 官方页面 Bad Request 的真实服务对照

用户补充的错误发生在 `us-east-1.signin.aws.amazon.com/oauth`，携带 `iam_user=true`、`backwards_compatible=true`。Brain 随后确认原始链接确实包含 43 字符的 `code_challenge` 和 `state`，`code_challenge_method=S256`；最初转述只是省略了 challenge。

在 2026-11-02 11:42–11:53（Asia/Shanghai）直接请求真实 AWS，并用隔离、未登录的 agent-browser 会话实际点击授权页的 Continue：

| 对照 | 真实 AWS 结果 |
| --- | --- |
| 用户原 client `dcr/99e3741b-a03b-4885-9b87-edbae3b15e89` + 完整参数 | `/v1/authorize` 302 → `/v1/sessions` 200；Continue 后出现 IAM 登录表单 |
| 新 DCR 注册，回调与用户链接完全相同 | 注册 HTTP 200，响应确认 `http://127.0.0.1:9876/plugins/oauth/callback`；授权和 Continue 同样到 IAM 登录表单 |
| 使用旧 client 重复打开同一未完成的授权链接 | 再次成功到 IAM 登录表单 |
| 旧 client + Chrome Mac User-Agent + zen Referer | HTTP 200 授权页面，两次一致 |
| 新 client + 切换 AWS 新登录体验 | 到 “Sign in to AWS” 邮箱登录页 |
| 旧 client + 在测试浏览器启用 AWS 多会话 | 到 IAM 登录表单 |
| 随机不存在的 DCR client | `/v1/authorize` HTTP 400 |
| 新 client，但回调改为未注册的另一个 loopback 端口 | `/v1/authorize` HTTP 400 |
| 不携带 `code_challenge` 的负例 | `/v1/authorize` HTTP 400；这不是用户实际请求的情况 |
| 尝试直接注册 zen 的 HTTPS 回调 | HTTP 400 `invalid_redirect_uri`，明确拒绝该 URI |
| 从真实 zen 页面点击 Sign in to AWS，然后点击 AWS Continue | 使用原 client 和完整九参数，最终出现 IAM 登录表单；已保存截图和脱敏参数记录 |

因此，**已保存 client 当前有效、注册的 loopback 回调与授权请求匹配、这组参数当前被 AWS 接受**。不能用“重新注册 client”或改为 zen HTTPS 回调来冒充已经确认的根因修复。测试仅创建了一个不含用户授权的公开 DCR client，没有完成 AWS 登录或修改 AWS 资源。

AWS 的回调白名单本身支持 localhost/127.0.0.1，并不接受任意应用域名；因此即使入口在 zen，Mewla 使用 loopback 并在异机登录时让用户粘回地址仍是当前的约束。参见 [AWS MCP OAuth 回调规则](https://docs.aws.amazon.com/signin/latest/userguide/aws-mcp-server.html)。注册接口的本次实测也直接证实 zen HTTPS 回调被拒绝。

发现 AWS 官方仓库中的 [aws/aws-cli#10186](https://github.com/aws/aws-cli/issues/10186)：已有用户报告过期浏览器会话导致同样的 `/v1/sessions → /oauth?iam_user=true&backwards_compatible=true → 400`；AWS 维护者确认错误来自 SignIn 服务，并将 issue 标记为 service-api。这与用户现象一致，但**仅为待验证候选原因**，不能代替用户浏览器的 A/B 证据。

已通过最新 turn 的 progress `--attention user_input` 请 Brain 让用户保留原 Mewla 页，用新发起的授权链接在隐私窗口试一次，并确认错误出现的具体一步。Mewla 本地 flow 过期/被取消会影响最终回调，本地状态本身不会使 AWS 已接受的公开 client 失效。浏览器授权完成后的重复提交尚未用真实账户测试。

本轮没有修改 OAuth 生产代码，没有清空有效注册，也没有加入盲目重复注册或私有 AWS 跳转参数。现有的过期注册替换、token endpoint `invalid_client` 后忘记注册，以及 callback 防重放测试定向复跑通过。

本轮证据（授权 URL 对照文件仅含本轮独立诊断生成的 state/PKCE challenge，没有用户授权码或令牌）：

- [真实 zen 发起并到达 IAM 登录表单](/home/daoleno/.mewla/t/RrrgZV/aws-authorize-evidence/live-actual-url-login.png)
- [旧 client 登录表单](/home/daoleno/.mewla/t/RrrgZV/aws-authorize-evidence/saved-login.png)
- [新 client 登录表单](/home/daoleno/.mewla/t/RrrgZV/aws-authorize-evidence/fresh-login.png)
- [HTTP 授权对照](/home/daoleno/.mewla/t/RrrgZV/aws-authorize-evidence/probe-results.json)
- [下一跳 oauth 对照](/home/daoleno/.mewla/t/RrrgZV/aws-authorize-evidence/hop-results.json)
- [新 DCR 返回及注册回调](/home/daoleno/.mewla/t/RrrgZV/aws-authorize-evidence/registration-response.json)
- [新 client 的完整诊断授权 URL](/home/daoleno/.mewla/t/RrrgZV/aws-authorize-evidence/fresh-full.url)
- [旧 client 的完整诊断授权 URL](/home/daoleno/.mewla/t/RrrgZV/aws-authorize-evidence/saved-full.url)
- [真实 zen 原始授权 URL 的脱敏 HTTP 证明](/home/daoleno/.mewla/t/RrrgZV/aws-authorize-evidence/live-actual-http-proof.json)
- [真实登录页九参数与 state/challenge 长度](/home/daoleno/.mewla/t/RrrgZV/aws-authorize-evidence/live-login-metadata.json)
- [现有登录/注册/回调测试](/home/daoleno/.mewla/t/RrrgZV/aws-authorize-evidence/existing-flow-tests.log)

![真实 zen 发起并到达 IAM 登录表单](/home/daoleno/.mewla/t/RrrgZV/aws-authorize-evidence/live-actual-url-login.png)

本轮收尾：已取消本 Worker 新发起的未完成登录，撤销新建测试浏览器配对并关闭三个测试浏览器；仅保留上述诊断证据。原有 AWS 账户仍为八条 disconnected，未删除或改写。等待 Brain 转回用户隐私窗口对照及具体失败步骤后继续定位；没有将任务置为 done。
