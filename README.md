# devcontainer-user-env

Dev Containerをターミナル中心で利用するための個人用環境です。

次の機能を提供します。

- `dc bash` で現在のリポジトリのDev Containerに入る
- `dc codex` や `dc <command>` でDev Container内のツールを実行する
- `dc rebuild` でDev Containerを再構築する
- ホストへ貼り付けたスクリーンショットをDev Containerから参照する
- `[HOST]` と `[DEV]` を明確に区別するシェルプロンプト
- Dev Container CLIのdotfiles機能を利用したインストール

開発対象のリポジトリに個人固有の変更を加える必要はありません。

## 必要なもの

- Bash
- Git
- [Dev Container CLI](https://github.com/devcontainers/cli)
- Docker、またはDev Container CLIが対応するコンテナランタイム
- [Starship](https://starship.rs/)（任意）
- [Tailscale](https://tailscale.com/)（Screenshot WebをTailnetへ公開する場合）

## ホストへのインストール

このリポジトリをcloneして、インストーラーを実行します。

```bash
git clone https://github.com/iuill/devcontainer-user-env.git \
  ~/.local/share/devcontainer-user-env
cd ~/.local/share/devcontainer-user-env
./install-host.sh
```

`~/.local/bin` が `PATH` に含まれていることを確認し、新しいシェルを
起動してください。

インストーラーは次の処理を行います。

1. `bin/dc` を `~/.local/bin` にシンボリックリンクする
2. `bin/build-screenshot-web` を `~/.local/bin` にシンボリックリンクする
3. Bashのプロンプトに黄色い `⚠️ HOST` バッジを表示する

## 使い方

Dev Container設定を含むリポジトリで実行します。

```bash
dc
dc bash
dc codex
dc npm test
dc rebuild
dc rebuild claude
```

`dc codex` では、ホスト側の `dc` と `devcontainer`、コンテナ内のCodexに
`HERDR_AGENT=codex` が設定されます。ホスト側で動くHerdrはこの値を使って、
Dev Containerの背後で動くCodexを検出できます。

`dc rebuild` は既存のDev Containerを削除して再構築した後、対話Bashを
起動します。コマンドを続けて指定すると、対話Bashの代わりにそのコマンドを
実行します。

```bash
dc rebuild claude
dc rebuild npm test
```

bind mountやnamed volumeに保存していないコンテナ内のデータは、再構築時に
失われます。

`dc` は次のようにコンテナを起動します。

```bash
devcontainer up \
  --workspace-folder "$PWD" \
  --dotfiles-repository <このリポジトリのURL> \
  --dotfiles-install-command install-devcontainer.sh
```

その後、指定されたコマンドを `devcontainer exec` で実行します。

コンテナ起動後は、コンテナ内の `~/dotfiles` を `git pull --ff-only` で
自動的に同期します。dotfilesが存在しない場合は自動的にcloneし、
`install-devcontainer.sh` を再適用してから指定されたコマンドを起動します。
同期に失敗した場合は警告を表示し、既存環境のまま処理を続行します。

dotfilesのGit URLには、このリポジトリの `origin` が自動的に使われます。
必要であれば環境変数で上書きできます。

```bash
export DC_DOTFILES_REPOSITORY=https://github.com/example/devcontainer-user-env.git
```

別のワークスペースを指定することもできます。

```bash
DC_WORKSPACE_FOLDER=/path/to/project dc bash
```

## Screenshot Web

Screenshot Webは、Windows PCやスマートフォンのブラウザから画像を貼り付け、
ホストの `~/screenshots` へ保存する小さなWebアプリです。保存した画像は、
`dc` が起動するすべてのDev Containerから `/screenshots` として参照できます。

WebアプリはGo標準ライブラリと素のHTML、CSS、JavaScriptだけで構成されています。
GoのDockerイメージを使って単一バイナリにするため、ホストへGoを
インストールする必要はありません。

### インストール

Dockerを利用できる状態で、次を実行します。

```bash
./install-screenshot-web.sh
```

このスクリプトは一時コンテナ内でテストとビルドを行い、生成したバイナリと
systemdユーザーサービスをインストールします。ビルド用コンテナは終了時に
削除されます。GoのDockerイメージは次回のビルド用にキャッシュされます。

標準では `golang:1.26-alpine` を使います。別のイメージを使う場合は、
ビルド時に指定できます。

```bash
SCREENSHOT_WEB_GO_IMAGE=golang:1.26-alpine ./install-screenshot-web.sh
```

### 起動とTailnetへの公開

Webアプリを現在のログイン時と今後のログイン時に起動します。

```bash
systemctl --user enable --now screenshot-web.service
```

systemdを使わないLinuxでは、通常のコマンドとして起動できます。

```bash
screenshot-web
```

アプリ自体は `127.0.0.1:3939` だけで待ち受けます。Tailscale Serveを使うと、
MagicDNSのホスト名を使ったHTTPSでTailnet内へ公開できます。

```bash
tailscale serve --bg --yes 3939
tailscale serve status
```

表示されたURLをWindows PCやスマートフォンで開き、ページ上で画像を
貼り付けるか、画像ファイルをドロップします。保存後に表示される
`/screenshots/<ファイル名>` をAIエージェントへ渡せます。

公開を解除する場合は次を実行します。

```bash
tailscale serve reset
```

### 保存先とDev Containerへのマウント

ホスト側の標準保存先は `$HOME/screenshots`、Dev Container側は
`/screenshots` です。ホスト側を変更する場合は、Webアプリの
`SCREENSHOT_WEB_DIR` と `dc` の `DC_SCREENSHOTS_DIR` に同じ絶対パスを
設定してください。

```bash
export SCREENSHOT_WEB_DIR=/mnt/data/screenshots
export DC_SCREENSHOTS_DIR=/mnt/data/screenshots
```

systemdサービスで標準以外の場所を使う場合は、サービスのdrop-inで
`SCREENSHOT_WEB_DIR` と書き込み許可パスを変更する必要があります。

すでに起動しているDev Containerへマウントを追加するには、一度再構築します。

```bash
dc rebuild
```

現在のDev Container CLIの `--mount` にはread-only指定がないため、
`/screenshots` はコンテナからも書き込み可能です。Web UI以外から画像を
変更したくない場合は、この制約に注意してください。

### Webアプリの制限

- 保存できる形式はPNG、JPEG、GIF
- 1ファイルの標準上限は20 MiB
- 画像の一辺は20,000ピクセルまで
- アプリはユーザーが指定したファイル名を保存先に使用しない
- 削除できるのは保存ディレクトリ直下の対応画像だけ

## プロンプトの色

- ホスト：黒背景の `⚠️` と、黄色背景に黒文字の `HOST` バッジ
- Dev Container：黒背景の `🐳` と、青背景に白文字の `DEV` バッジ
- rootシェル：黒背景の `🧨` と、赤背景に白文字の `ROOT` バッジ

DEVプロンプトは `/.dockerenv` も判定します。そのため、コンテナ内で新たに
対話シェルを起動した場合も `🐳 DEV` バッジが表示されます。

Starshipがインストールされている場合は、ホストで `starship-host.toml`、
Dev Containerで `starship.toml` を自動的に使います。Starshipがない場合は
Bashの `PS1` に直接バッジを追加します。

ホスト側では、`~/src` 配下のmain checkoutとworktreeを識別しやすいように
ディレクトリ名を整形します。

```text
~/src/narou-viewer/main
→ narou-viewer/main

~/src/narou-viewer/worktrees/remove-legacy-dev-prompt
→ narou-viewer/remove-legacy-dev-prompt
```

`src` の場所が `~/src` ではない場合は、`DEVCONTAINER_SRC_ROOT` で指定できます。

```bash
export DEVCONTAINER_SRC_ROOT=/path/to/src
```
