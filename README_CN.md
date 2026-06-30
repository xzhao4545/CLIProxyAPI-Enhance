# CLIProxyAPI-Enhance

[English](README.md) | [日本語](README_JA.md)

本仓库 Fork 自 [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI)，在其基础上主要增加了以下功能：

## 新增功能

### 使用统计与持久化
- 内置基于 SQLite 的请求用量统计持久化，支持按 provider、model、auth 维度的聚合查询
- 提供 `/v0/management/usage` 等统计查询 API
- 支持按时间范围、provider、模型、auth 等条件筛选的统计页面

### 响应关键字过滤
- 可在上游流式响应中检测配置的关键字，用于识别供应商额度、策略限制或自定义失败文本
- 支持 OpenAI Chat Completions、OpenAI Responses/Codex、Anthropic/Claude 和 Gemini 兼容流式格式
- 命中后会以 `keyword_filtered` 记录为失败用量，并包含命中的关键字和受限长度的响应上下文
- 有可用备用供应商时，命中失败可触发供应商故障转移和冷却
- 可通过 `/v0/management/keyword-filters` 或管理面板维护规则

### Codex Responses 重试过滤
- 面向 Codex/OpenAI Responses 协议的临时重试保护，用于处理特定 reasoning token 长度下的异常完成模式
- 支持配置启用状态、模型 glob 匹配、命中 reasoning token 长度、流式/非流式拦截以及保护重试次数
- 默认匹配 `gpt-*` 模型以及 `516`、`1034`、`1552` 三个 reasoning token 长度
- 命中后会在内部或调度器中自动重试，不会把合成的重试错误返回给客户端
- 基于 SQLite 记录检查次数、命中次数、命中率、重试成功率、命中长度、动作、认证标签和最近命中详情
- 可通过 `/v0/management/codex-response-retry-filter` 或配套管理面板页面进行配置和查看
- 该功能需要使用配套前端
  [xzhao4545/Cli-Proxy-API-Management-Center-Ehance](https://github.com/xzhao4545/Cli-Proxy-API-Management-Center-Ehance)
  中对应实现，包括 `src/pages/CodexRetryFilterPage.tsx`、`src/pages/CodexRetryFilterPage.module.scss`
  以及相关语言包条目。旧版本前端不包含该页面。

### 提供商自定义标签
- 可为 AI 提供商设置自定义名称（`label` 字段）
- 在管理面板提供商列表中显示为标签名，未设置时自动生成 `{brand}#{序号}` 格式

### 默认前端管理面板
- 默认内置的管理面板地址已指向本项目的配套前端：
  [xzhao4545/Cli-Proxy-API-Management-Center-Ehance](https://github.com/xzhao4545/Cli-Proxy-API-Management-Center-Ehance)
- 前端通过 `/v0/management/*` 接口与后端交互，提供配置管理、提供商管理、用量统计等功能

## 配置

```yaml
# 使用统计持久化配置
usage:
  enabled: true                    # 启用 SQLite 持久化统计
  sqlite-path: ./data/usage.db     # 数据库路径（默认如上）

# 响应关键字过滤配置
keyword-filters:
  - keyword: "insufficient credits"
    match-mode: "anywhere"         # anywhere、start、end、exact
    case-sensitive: false
    enabled: true

# Codex/OpenAI Responses 重试过滤配置
codex-response-retry-filter:
  enabled: false
  models:
    - "gpt-*"
  reasoning-token-lengths:
    - 516
    - 1034
    - 1552
  intercept-streaming: true
  intercept-non-streaming: true
  guard-retry-attempts: 3

# 远程管理面板地址配置
remote-management:
  panel-github-repository: https://github.com/xzhao4545/Cli-Proxy-API-Management-Center-Ehance
  disable-auto-update-panel: false # 是否禁用面板自动更新
```

## 截图

### Codex Responses 重试过滤

![Codex Responses 重试过滤](img/516-retry.png)

### 使用统计

![使用统计](img/使用统计页.png)

## 上游文档

原项目功能（多账户负载均衡、OAuth 认证、Amp CLI 集成等）请参考：
- https://github.com/router-for-me/CLIProxyAPI

## License

MIT
