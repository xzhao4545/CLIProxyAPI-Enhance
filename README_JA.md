# CLIProxyAPI-Enhance

[English](README.md) | [中文](README_CN.md)

このリポジトリは [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) のフォークであり、以下の主要な機能が追加されています。

## 機能

### 使用統計と永続化
- SQLite ベースのリクエスト使用統計の永続化を内蔵。provider、model、auth 単位での集計クエリをサポート
- `/v0/management/usage` などの統計クエリ API を提供
- 時間範囲、provider、モデル、auth などでフィルタリング可能な統計ページ

### レスポンスキーワードフィルタリング
- 上流のストリーミングレスポンス内に設定済みキーワードを検出し、プロバイダーのクォータ、ポリシー制限、またはカスタム失敗テキストを識別可能
- OpenAI Chat Completions、OpenAI Responses/Codex、Anthropic/Claude、Gemini 互換のストリーミング形式をサポート
- マッチしたレスポンスは `keyword_filtered` として失敗使用量に記録され、マッチしたキーワードと制限されたレスポンスコンテキストを含む
- 代替プロバイダーが利用可能な場合、マッチによる失敗はプロバイダーのフェイルオーバーとクールダウンをトリガー可能
- `/v0/management/keyword-filters` または管理パネルからルールを管理可能

### Codex Responses リトライフィルター
- Codex/OpenAI Responses プロトコル向けの一時的なリトライガードで、特定の reasoning token 長で発生する異常な完了パターンを扱うための機能
- 有効化、モデル glob マッチ、マッチ対象 reasoning token 長、ストリーミング/非ストリーミングのインターセプト、ガードリトライ回数を設定可能
- デフォルトでは `gpt-*` モデルと `516`、`1034`、`1552` の reasoning token 長を対象
- マッチしたリクエストは内部または conductor 経由で自動的にリトライされ、合成されたリトライエラーはクライアントへ返されません
- SQLite にチェック回数、ヒット数、ヒット率、リトライ成功率、マッチ長、アクション、認証ラベル、最近のヒット詳細を記録
- `/v0/management/codex-response-retry-filter` または対応する管理パネルページから設定・確認可能
- この機能には、対応するフロントエンド
  [xzhao4545/Cli-Proxy-API-Management-Center-Ehance](https://github.com/xzhao4545/Cli-Proxy-API-Management-Center-Ehance)
  の実装が必要です。対象ファイルは `src/pages/CodexRetryFilterPage.tsx`、
  `src/pages/CodexRetryFilterPage.module.scss`、および関連するロケールエントリです。古いフロントエンドビルドにはこのページが含まれていません。

### プロバイダーカスタムラベル
- AI プロバイダーにカスタム名（`label` フィールド）を設定可能
- 管理パネルのプロバイダー一覧にラベル名として表示。未設定の場合は `{brand}#{番号}` 形式で自動生成

### デフォルト管理パネル
- 内蔵管理パネルの URL は本プロジェクトのフロントエンドを指しています：
  [xzhao4545/Cli-Proxy-API-Management-Center-Ehance](https://github.com/xzhao4545/Cli-Proxy-API-Management-Center-Ehance)
- フロントエンドは `/v0/management/*` API を介してバックエンドと連携し、設定管理、プロバイダー管理、使用統計などを提供

## 設定

```yaml
# 使用統計の永続化設定
usage:
  enabled: true                    # SQLite 永続化統計を有効化
  sqlite-path: ./data/usage.db     # データベースのパス（デフォルトは上記）

# レスポンスキーワードフィルタリング設定
keyword-filters:
  - keyword: "insufficient credits"
    match-mode: "anywhere"         # anywhere、start、end、exact
    case-sensitive: false
    enabled: true

# Codex/OpenAI Responses リトライフィルター設定
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

# リモート管理パネルの設定
remote-management:
  panel-github-repository: https://github.com/xzhao4545/Cli-Proxy-API-Management-Center-Ehance
  disable-auto-update-panel: false # パネルの自動更新を無効にするかどうか
```

## スクリーンショット

### Codex Responses リトライフィルター

![Codex Responses リトライフィルター](img/516-retry.png)

### 使用統計

![使用統計](img/使用统计页.png)

## アップストリームドキュメント

元のプロジェクトの機能（マルチアカウント負荷分散、OAuth 認証、Amp CLI 統合など）については以下を参照してください：
- https://github.com/router-for-me/CLIProxyAPI

## ライセンス

MIT
