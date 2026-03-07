# Codex CLI / Claude Code 接入 ds2api 稳定性测试报告（2026-03-07）

## 测试目标

1. 安装 `codex` 与 `claude` 两个编码工具 CLI。
2. 将两者接入本地 `ds2api` 作为提供商。
3. 执行实际“代码编写类任务”（读取仓库信息 + 调用本地工具），观察是否稳定。
4. 开启 `ds2api` 抓包后定位异常点。

## 环境与配置

- ds2api 启动参数包含：
  - `DS2API_DEV_PACKET_CAPTURE=1`
  - `DS2API_DEV_PACKET_CAPTURE_LIMIT=500`
  - `DS2API_DEV_PACKET_CAPTURE_MAX_BODY_BYTES=250000`
  - `DS2API_ADMIN_KEY=admin`
- Codex CLI 使用：
  - `CODEX_HOME=/tmp/codex-home`
  - `OPENAI_BASE_URL=http://172.31.7.18:5001/v1`
  - 已通过 `codex login --with-api-key` 写入 key。
- Claude Code 使用：
  - `ANTHROPIC_BASE_URL=http://172.31.7.18:5001/anthropic`
  - `ANTHROPIC_API_KEY=<ds2api key>`

## 结果摘要

### 1) Codex CLI：已接入并可执行编码任务，但出现流式事件解析异常

- Codex 成功执行 shell 工具（`pwd`、`head -n 1 README.MD`），能返回结果。
- 但同一轮输出中出现多次：
  - `OutputTextDelta without active item`
- 现象是**最终答案出现重复输出**（同样两条 bullet 在输出尾部重复一次）。

### 2) Claude Code：在本环境中未能成功走到 ds2api 请求链路

- `claude -p` 在 20s 超时内没有正常产出。
- debug 日志显示它仍在进行 Anthropic 自身 OAuth/组织状态流程，且请求 `api.anthropic.com` 返回 401。
- 关键日志：
  - `has Authorization header: false`
  - `API error ... Connection error`
  - `https://api.anthropic.com/api/claude_code/organizations/metrics_enabled ... 401`

结论：当前 Claude Code CLI 行为不是“纯 API key + 自定义 base_url”直连模式，因此本次无法稳定完成 ds2api 直连验证。

## 抓包与代码分析结论

### A. ds2api 上游抓包（DeepSeek）未见明显异常

在 `/admin/dev/captures` 抓包中，Codex 对应的 DeepSeek completion 返回均为 200，且响应分片结构完整（`ready/update_session/finish` 链完整）。

### B. ds2api `/v1/responses` 事件顺序自检正常

手动对 `/v1/responses` 做 SSE 验证时，事件顺序为：

1. `response.created`
2. `response.output_item.added`
3. `response.content_part.added`
4. `response.output_text.delta`...
5. `response.content_part.done`
6. `response.output_item.done`
7. `response.completed`

这满足“delta 前先有 active item”的基本契约。

### C. 当前更像 Codex CLI 客户端兼容问题而非 ds2api 上游中断

由于：
- ds2api 发出的 Responses 事件顺序在 curl 复测下正常；
- DeepSeek upstream 抓包也正常；
- 异常日志由 Codex 客户端本身报出（`codex_core::util`）。

因此本轮判断：**主要问题在 Codex CLI 对某些 Responses 流片段的容错处理**，而不是 ds2api 到 DeepSeek 的链路不稳定。

## 建议下一步

1. 新增一个“Codex 兼容追踪”开关（debug 模式）记录 `/v1/responses` 对外 SSE 原文（仅本地开发可开），便于直接与 Codex 报错时间点对齐。
2. 针对 Codex 的报错场景，增加 Responses 兼容回归测试：
   - 工具执行后再文本总结的混合流；
   - 连续多个 `output_text.delta` 与 `content_part.done` 边界。
3. 对 Claude Code：优先确认其支持的“非 Anthropic 官方 host”认证模式（若无，则应改为通过其官方支持的代理模式接入）。
