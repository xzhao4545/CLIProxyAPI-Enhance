# CLIProxyAPI-Enhance

[English](README.md) | [中文](README_CN.md)

このリポジトリは [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) のフォークであり、以下の主要な機能が追加されています。

## 機能

### 使用統計と永続化
- SQLite ベースのリクエスト使用統計の永続化を内蔵。provider、model、auth 単位での集計クエリをサポート
- `/v0/management/usage` などの統計クエリ API を提供
- 時間範囲、provider、モデル、auth などでフィルタリング可能な統計ページ

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

# リモート管理パネルの設定
remote-management:
  panel-github-repository: https://github.com/xzhao4545/Cli-Proxy-API-Management-Center-Ehance
  disable-auto-update-panel: false # パネルの自動更新を無効にするかどうか
```

## アップストリームドキュメント

元のプロジェクトの機能（マルチアカウント負荷分散、OAuth 認証、Amp CLI 統合など）については以下を参照してください：
- https://github.com/router-for-me/CLIProxyAPI

## ライセンス

MIT
