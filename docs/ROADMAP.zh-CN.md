# 路线图

agent-handoff 的目标，是让用户在换人、换机器、换编码智能体时仍然像在继续同一个任务，
同时不把私有对话变成必须托管到云账号的数据。路线图围绕用户结果组织，不是固定功能清单，
也不是发版承诺。

## 当前安排（2026-11-19）

- 维护现有 Codex 和 Claude Code 交接路径。
- 保留 [DeepSeek Harness 接入研究（#10）](https://github.com/DavidDingXu/agent-handoff/issues/10)，包括评估通过宿主官方会话服务实现 DSH 原生插件。这是待验证的接入方案，尚未实现，也不构成交付承诺。
- [OpenCode 接入（#9）](https://github.com/DavidDingXu/agent-handoff/issues/9)、[安装与 provider 诊断（#11）](https://github.com/DavidDingXu/agent-handoff/issues/11)、[更完整的分享前预览（#12）](https://github.com/DavidDingXu/agent-handoff/issues/12) 暂缓，三个 issue 当前按 `not_planned` 关闭，后续调整优先级时可以重新打开。

## 产品原则

- 接收方得到可继续的原生新任务，而不是一份转录文本。
- 默认路径不要求账号、部署或 provider token。
- 分享链接只上传本机加密后的密文，明文留在本机。
- 导入始终只追加，不改写已有任务。
- 优先使用配置而不是可执行插件；只有多个真实接入证明接口稳定后，才增加运行时扩展点。
- macOS、Linux 和 Windows 都是一等支持目标。

## 持续维护：让现有交接稳定成功

**用户结果：** 新用户只看 README，就能在 Codex 或 Claude Code 中安装插件并完成文件或
加密链接交接，不需要先理解实现细节。

已经交付的基础：

- 提供声明式 HTTP provider 配置，让已有对象存储、企业文件平台或国内文件服务的用户
  可以直接接入。
- 公共 Worker 和匿名 provider 池继续作为零配置、尽力而为的默认路径，最终始终保留
  本地 zip 兜底。
- 在主 README 增加真实端到端使用截图、浅色架构图和最短排障入口。
- 简化公共 Worker，只保留文件大小与过期清理两类运行约束，并通过部署配置统一管理。

成功信号：

- 每次发版都通过 Codex 和 Claude Code 插件安装冒烟测试。
- 文件与链接交接在 macOS、Linux、Windows 上通过四象限测试。
- 链接服务不可用时，总能返回其他有效加密路径或明确的本地 zip。
- 新用户只依赖 README 就能完成一次冷启动交接。

## 研究方向：DeepSeek Harness 接入

**用户结果：** 用户可以在日常使用的多个编码智能体之间迁移真实任务，并保留目标宿主的
原生继续体验。

围绕 **[DeepSeek Harness（`dsh`）](https://github.com/deepseek-ai/deepseek-harness)**（[#10](https://github.com/DavidDingXu/agent-handoff/issues/10)）的固定版本和已记录 fixture 开展验证。DSH 原生插件是宿主交互与会话访问的候选方案，agent-handoff 继续负责捆绑包、中立转录、加密和传输合同。先验证只读检查与导出，涉及导入时先使用临时隔离配置，再评估正式支持。

DSH 仍处于开发者预览期。正式接入需要会话和存储 API 支持安全原生恢复的证据；
可以导出日志本身不能证明它能在另一台宿主上继续。

每增加一个 Agent，都必须满足同一份验收合同：

- 同 Agent 交接在新身份下保留原生事件。
- 跨 Agent 通过版本化中立转录保留可见对话与工具证据。
- 导入只创建一个新任务，支持预览、重复检测，并能从目标宿主重新打开验证。
- CI 跑完整 N x N 矩阵：3 个 Agent 是 9 条路径，4 个 Agent 是 16 条路径。

只有第三个真实实现证明共用合同后，才提取 Agent adapter 边界。
届时由真实差异决定 adapter SDK，而不是先设计一个通用接口再强迫各家适配。

## 暂未排期的候选：让高频交接更清楚

**用户结果：** 团队能把交接用于日常开发，并且在上传前知道会分享什么、链接能用多久，
失败后也能马上知道问题在哪。

以下候选当前没有交付排期：

- 暂缓：[更完整的分享前预览（#12）](https://github.com/DavidDingXu/agent-handoff/issues/12)，包括消息数、图片、改动文件证据、敏感信息、预计上传大小、分享路径
  与过期时间。
- 创建捆绑包前提供引导式脱敏和附件选择，同时不改动原任务。
- 提供可被 Agent 原生 UI 渲染的结构化进度与重试信息，并为自动化提供稳定错误码。
- 暂缓：[provider 配置诊断（#11）](https://github.com/DavidDingXu/agent-handoff/issues/11)，以及经过审核的 S3 兼容、企业和区域文件服务配置样例。
- 为已支持的 Codex、Claude Code 会话格式保留兼容性 fixture；其他宿主在接入获准后再覆盖，让宿主升级引发的问题先在 CI 暴露。

成功信号：

- 用户确认上传前能明确看到分享范围与过期时间。
- provider 或配置失败能定位到具体字段或 HTTP 阶段。
- 兼容策略覆盖范围内的每个宿主历史版本都有 fixture 回放。

## 更远：安全的扩展生态

**用户结果：** 贡献者能增加 Agent 和组织定制策略，同时不削弱核心安全与可移植性保证。

- 至少三个原生 adapter 验证接口后，发布 adapter 一致性测试套件，包括 fixture、N x N
  测试和宿主校验钩子。
- 用声明式策略配置组织级脱敏规则、保留期限边界和允许的链接路径。
- 维护经过审核的 provider 配置样例库；provider 继续是数据配置，不变成下载后执行的代码。
- 评估签名来源与可选组织信任策略，但不强制中心账号，也不破坏匿名分享能力。

## 明确不做

- 服务端存储明文对话，或强制注册 agent-handoff 账号。
- 任意执行的文件 provider 插件。
- 在第三个原生 Agent 接入前设计万能 adapter 抽象。
- 对私有编码会话做静默遥测。质量证据来自用户主动提交的问题、兼容性 fixture 和 CI。

## 如何提交路线图建议

Feature Request 应写清用户问题、目标 Agent 或服务、原生会话证据和验收示例。可以提出
具体功能，但优先级取决于它解决的用户结果，以及是否保持上面的产品不变量。
