<div align="center">
<img alt="Northrou" src="public/repo/Hero_Banner_JPG_v1.2__Northrou.jpg" width="100%">
</div>

<h3 align="center">Northrou</h3>

<p align="center">
  <a href="README.md">English</a> ·
  <a href="README.zh-CN.md">简体中文</a> ·
  <a href="README.es.md">Español</a> ·
  <a href="README.fr.md">Français</a> ·
  <a href="README.de.md">Deutsch</a> ·
  日本語
</p>

<p align="center">自分のハードウェアから、自分の映画やドラマをストリーミング。</p>

<p align="center">
  <a href="https://northrou.sh">ウェブサイト</a> ·
  <a href="https://northrou.sh/docs">ドキュメント</a> ·
  <a href="#インストール">インストール</a> ·
  <a href="#ライセンス">ライセンス</a>
</p>

<p align="center">
<a href="https://github.com/rhymeswithlimo/northrou/releases"><img src="https://img.shields.io/github/v/release/rhymeswithlimo/northrou" alt="Latest release"></a>
<a href="LICENSE"><img src="https://img.shields.io/badge/license-BSD--3--Clause-blue" alt="License: BSD 3-Clause"></a>
<a href="https://github.com/rhymeswithlimo/northrou/commits/main"><img src="https://img.shields.io/github/last-commit/rhymeswithlimo/northrou" alt="Last commit"></a>
<img src="https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.26+">
</p>

---

Northrou は、自分のハードウェア上で動かすオープンソースのメディアサーバーです。映画やドラマのライブラリを指定するだけで、自宅でも外出先でも、スマートフォン、タブレット、デスクトップ、テレビへとストリーミングできます。メディアが他人のサーバーを経由することは一切ありません。

再生は視聴するデバイスに合わせて自動的に最適化されます。デバイスが直接処理できる場合、ファイルは無加工のまま再生され、実際に必要な場合のみ変換が行われ、利用可能であれば GPU も活用されます。Dolby Atmos やロスレス音声トラックはステレオに落とし込まれることなく、そのまま、またはデバイスごとに適応して伝送されます。

ライブラリを追加すれば、あとは Northrou にお任せです。ポスター、キャスト、詳細情報は自動的にマッチングされ、字幕（ほとんどのサーバーが扱えない画像ベースの字幕を含む）もそのまま利用でき、あなた自身の視聴履歴（どこにも共有されることはありません）から構築されたレコメンドエンジンが、次に見るべき作品を見つける手助けをします。

サーバーのセットアップは一人が一度行うだけで、接続コードを共有します。他の人はそのコードをアプリに入力するだけで接続できます。アカウントもメールアドレスもパスワードも不要です。リモートアクセスはピアツーピアで行われ、サーバーとデバイスが直接通信するため、途中で誰かが視聴内容を見ることは一切ありません。

## インストール

```sh
curl -sSL https://raw.githubusercontent.com/rhymeswithlimo/northrou/main/scripts/install.sh | sh
northrou setup
```

インストーラーは Northrou をバックグラウンドサービスとして設定し、FFmpeg を自動的に取得します。それ以外に何もインストールする必要はありません。続いて `setup` が、ブラウザを一切使わずターミナル上だけで、サーバー名の設定、メディアフォルダの追加、接続コードの生成まで案内します。他のデバイスにアプリをインストールしてコードを入力すれば、接続完了です。

Docker を使いたい、あるいは手動でインストールしたい場合は？ すべてのインストール方法と設定オプションを含む完全なガイドは [northrou.sh/docs](https://northrou.sh/docs)、またはこのリポジトリの [docs/](docs/) にあります。

## コマンド

日常的にはこのほとんどを使う必要はありません。中身を覗いてみたいときは、`northrou admin` でストリーム、ハードウェア、キャパシティをリアルタイムに表示するターミナルダッシュボードが開きます。

```text
northrou <command> [flags]

COMMANDS:
   setup                    ターミナルでサーバーをセットアップ（名前、メディア、TMDB キー、コード）
   status                   サーバーの現在の状態と次にすべきことを表示
   doctor                   設定を確認し、問題があれば報告
   serve                    サーバーをフォアグラウンドで実行（サービスが実際に呼び出すコマンド）
   install / uninstall      システムサービスを登録 / 削除
   start / stop / restart   インストール済みサービスを制御
   logs                     サーバーの最近のログを表示、またはリアルタイムで追跡
   admin                    ライブ管理ダッシュボードを開く（TUI）
   scan [path...]           フォルダやドライブを今すぐスキャン（パス省略時は設定済みディレクトリをスキャン）
   rescan [path...]         すべてのファイルのメタデータを再取得（変更のないファイルも含む）
   match <file>             ファイルを特定の TMDB タイトルに強制的に一致させる
   backfill-metadata        既存タイトルのキーワード/制作会社/クリエイターを取得（レコメンドを改善）
   cc                       このサーバーの接続コードを表示（アプリのペアリング用）
   cc rotate                接続コードを再発行し、すべてのデバイスをサインアウトさせる
   devices                  ペアリング済みデバイスの一覧を表示
   devices revoke <id>      特定のペアリング済みデバイスをサインアウトさせる
   tmdb-key                 TMDB API キーを表示、設定、または削除
   update                   新しいバージョンを確認してインストール
   version                  バージョン情報を表示
   -h, --help               コマンドのヘルプを表示

GLOBAL（すべてのコマンド共通）:
   --config string          config.toml へのパス（デフォルト：OS の設定ディレクトリ）
   -v, --verbose             デバッグログを有効化

LOGS:
   -f, --follow             新しいログ行が出力されるたびに表示し続ける
   -n, --lines int          表示する末尾の行数（デフォルト 200）

ADMIN:
   --addr string            サーバーのベース URL（デフォルトは設定ファイルの値、例: http://localhost:8674）

SCAN / RESCAN:
   --tv                     指定したパスを TV エピソードとして扱う（デフォルト：ファイル名から自動判定）

MATCH:
   --tmdb-id int            紐づける映画・番組の TMDB ID（必須）
   --tv                     ファイルを TV エピソードとして扱う
   --season int             シーズン番号（--tv と併用）
   --episode int            エピソード番号（--tv と併用）

CC ROTATE:
   -y, --yes                確認なしでコードを再発行する

UPDATE:
   -y, --yes                確認なしでアップデートを適用する
   --check                  確認のみ行い、インストールはしない
```

`setup` は対話式で、フラグは不要です。サービス関連のコマンド（`install`、`start`、`stop`、`restart`、`update`）は root 権限が必要な場所に書き込むため、Linux では `sudo` を付けて実行してください。昇格が必要な場合はコマンド側から知らせてくれます。

## ドキュメント

設定オプション、HTTP API、アーキテクチャなどすべてを網羅した完全なリファレンスは [northrou.sh/docs](https://northrou.sh/docs) にあります。同じページはこのリポジトリ内にもミラーされています。

- [設定リファレンス](docs/configuration.md)
- [HTTP API リファレンス](docs/api.md)
- [アーキテクチャ](docs/architecture.md)
- [クライアント](docs/frontend.md)

## 開発

Northrou は完全にオープンソースで、自分でビルドすることもできます。モノレポ構成で、サーバーとリモートアクセス用ブローカーは別々の Go モジュールであり、クライアント（`frontend/`）は Web、デスクトップ、iOS、Android で共有される Tauri アプリです。

```sh
make build   # クライアントをビルドし、続けて bin/northrou と bin/coordinator をビルド
make test    # テストスイートを実行
make run     # ビルドしてサーバーをローカルで実行
```

各部分がどのように組み合わさっているかは [docs/architecture.md](docs/architecture.md) を参照してください。

## ライセンス

BSD 3-Clause。詳細は [LICENSE](LICENSE) を参照してください。この条件の下で、ソフトウェアを自由にビルド、実行、フォーク、再配布できます。**Northrou** の名称、ロゴ、ブランド資産はこの許諾の対象外であり、許可なく派生製品の推奨や宣伝に使用することはできません（詳細は [NOTICE](NOTICE) を参照）。
