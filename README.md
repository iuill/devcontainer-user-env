# devcontainer-user-env

Dev Containerをターミナル中心で利用するための個人用環境です。

次の機能を提供します。

- `dc bash` で現在のリポジトリのDev Containerに入る
- `dc codex` や `dc <command>` でDev Container内のツールを実行する
- `dc rebuild` でDev Containerを再構築する
- ホストへ貼り付けた画像や長いテキストをDev Containerから参照する
- `[HOST]` と `[DEV]` を明確に区別するシェルプロンプト
- Dev Container CLIのdotfiles機能を利用したインストール

開発対象のリポジトリに個人固有の変更を加える必要はありません。

## 必要なもの

- Bash
- Git
- [Dev Container CLI](https://github.com/devcontainers/cli)
- Docker、またはDev Container CLIが対応するコンテナランタイム
- [Starship](https://starship.rs/)（任意）
- [Tailscale](https://tailscale.com/)（Agent InboxをTailnetへ公開する場合）

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
2. Bashのプロンプトに黄色い `⚠️ HOST` バッジを表示する

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

ホスト側の `TERM` が空、`dumb`、`unknown` のいずれでもない場合は、Dev
Containerで `TERM=xterm-256color` を設定します。`COLORTERM` は、ホスト側で
設定されている場合にその値を引き継ぎます。CodexなどのTUIはこれらを使って
端末の色表現能力を判定します。

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

## Windows TerminalでCodex CLIへ改行を送る

Codex CLIでは `Shift+Enter` が改行に割り当てられていますが、端末が修飾された
Enterを区別して送信しない環境では通常のEnterとして扱われます。Windows Terminal
からPowerShellとSSHを経由してCodex CLIを使う場合は、Windows Terminal側で
`Shift+Enter` をLFとして送信します。

Windows Terminalで `Ctrl+,` を押して設定を開き、左下の「JSON ファイルを開く」
から `settings.json` を編集します。既存の `actions` と `keybindings` 配列へ、
それぞれ次の要素を追加します。

```json
{
  "actions": [
    {
      "command": {
        "action": "sendInput",
        "input": "\u000A"
      },
      "id": "User.sendLF"
    }
  ],
  "keybindings": [
    {
      "id": "User.sendLF",
      "keys": "shift+enter"
    }
  ]
}
```

これにより、`Shift+Enter` はSSH越しでもCodex CLIの `Ctrl+J` と同等の改行として
扱われます。Codex CLIで `/keymap` を開き、`Debug` の `Inspect keypresses` を
使うと、端末から受信したキーを確認できます。

Windows Terminalの `sendInput` については
[Windows Terminal Actions](https://learn.microsoft.com/windows/terminal/customize-settings/actions#send-input)
を参照してください。

## Agent Inbox

Agent Inboxは、Windows PCやスマートフォンのブラウザから画像や長いテキストを
貼り付け、ホストの `~/agent-inbox` へ保存する小さなWebアプリです。保存した
ファイルは、`dc` が起動するすべてのDev Containerから `/inbox` として
参照できます。チャットへ直接収まらないテキストをファイルとしてAIエージェントへ
渡す用途に加え、ホストの `~/src` 配下で生成されたファイルをブラウザから確認する
用途にも使えます。

WebアプリはGo標準ライブラリと素のHTML、CSS、JavaScriptだけで構成されています。
GoのDockerイメージを使って単一バイナリにするため、ホストへGoを
インストールする必要はありません。

### インストール

Dockerを利用できる状態で、次を実行します。

```bash
./install-agent-inbox.sh
```

このスクリプトは一時コンテナ内でテストとビルドを行い、生成したバイナリと
systemdユーザーサービスをインストールします。ビルド用コンテナは終了時に
削除されます。GoのDockerイメージは次回のビルド用にキャッシュされます。

ビルドコンテナでは、次の確認とビルドを順番に実行します。

1. `go test ./agent-inbox`
2. `go vet ./agent-inbox`
3. `agent-inbox` 以下のGoファイルが `gofmt` 済みであることを確認
4. `go build` で `build/agent-inbox` を生成

開発時にテストとビルドだけを実行する場合は、次を使えます。

```bash
./bin/build-agent-inbox
```

標準ではダイジェストを固定した `golang:1.26-alpine` を使います。別の
イメージを使う場合は、ビルド時に指定できます。

```bash
AGENT_INBOX_GO_IMAGE=golang:1.26-alpine ./install-agent-inbox.sh
```

### 起動とTailnetへの公開

Webアプリを現在のログイン時と今後のログイン時に起動します。

```bash
systemctl --user enable --now agent-inbox.service
```

systemdを使わないLinuxでは、通常のコマンドとして起動できます。

```bash
agent-inbox
```

アプリ自体は `127.0.0.1:3939` だけで待ち受けます。Tailscale Serveを使うと、
MagicDNSのホスト名を使ったHTTPSでTailnet内へ公開できます。

```bash
tailscale serve --bg --yes 3939
tailscale serve status
```

表示されたURLをWindows PCやスマートフォンで開き、ページ上で画像または
テキストを貼り付けます。画像・テキストファイルのドロップにも対応しています。
UTF-8テキストファイルは `.txt`、`.md`、`.json`、`.yaml`、`.yml`、`.csv`、
`.log`、`.diff`、`.patch` に対応し、元の拡張子を維持します。ブラウザには
内容を実行しないプレーンテキストとして配信します。
保存後は、ホスト用の `$HOME/agent-inbox/<ファイル名>` とDev Container用の
`/inbox/<ファイル名>` が表示されます。渡すAIエージェントの実行環境に合わせて
`HOST パス` または `DEV パス` をコピーできます。
スマートフォンではテキスト入力欄を長押しして貼り付けるか、
「クリップボードから貼り付け」ボタンを使用します。

ページ上部の「srcブラウザ」タブでは、ホストの `~/src` 配下をフォルダツリーと
ファイル一覧で移動し、ファイルのHOSTパスをコピーできます。PNG、JPEG、GIF、WebP、AVIF、SVGは
サムネイルと拡大表示に対応します。ソースコードや設定ファイルはブラウザで実行せず、
プレーンテキストとして表示します。それ以外のファイルはダウンロードできます。

srcブラウザは読み取り専用です。パストラバーサルを拒否し、隠しファイルと
シンボリックリンクは一覧表示も配信もしません。標準の閲覧ルートを変更する場合は、
systemdサービスのdrop-inで `AGENT_INBOX_SOURCE_DIR` を設定します。

```ini
[Service]
Environment=AGENT_INBOX_SOURCE_DIR=/mnt/data/src
```

ホスト上のCLIからUTF-8テキストファイルを保存することもできます。

```bash
curl -sS --fail -X POST http://127.0.0.1:3939/api/texts \
  -H 'X-Agent-Inbox: 1' \
  -H 'Content-Type: text/plain; charset=utf-8' \
  --data-binary @notes.txt
```

Tailscale ServeはTailnet全体へ公開します。Tailnetに他のユーザーや共有ノードが
存在する場合、そのままでは全員が閲覧・保存・削除できます。アクセスする
Tailscaleユーザーを限定する場合は、systemdサービスのdrop-inで
`AGENT_INBOX_ALLOWED_USER` を設定します。

```ini
[Service]
Environment=AGENT_INBOX_ALLOWED_USER=you@example.com
```

Agent Inboxにはインターネット公開用の認証機能がありません。
**`tailscale funnel` は使用しないでください。**

srcブラウザを有効にすると、隠しファイル以外のソースコードをTailnet経由で
閲覧できます。`~/src` に機微情報を含む場合は、`AGENT_INBOX_ALLOWED_USER` で
アクセスするTailscaleユーザーを必ず限定してください。

ユーザー制限はTailscale Serveが付与するIdentity Headerを検証します。
Identity Headerが付かないtagged nodeからのアクセスは拒否されます。
localhostから直接アクセスできるプロセスはヘッダを任意に指定できるため、
この制限は同一ホスト上のプロセスに対する認証ではありません。

公開を解除する場合は次を実行します。

```bash
tailscale serve reset
```

### 保存先とDev Containerへのマウント

ホスト側の標準保存先は `$HOME/agent-inbox`、Dev Container側は
`/inbox` です。ホスト側を変更する場合は、Webアプリの
`AGENT_INBOX_DIR` と `dc` の `DC_AGENT_INBOX_DIR` に同じ絶対パスを
設定してください。

```bash
export AGENT_INBOX_DIR=/mnt/data/agent-inbox
export DC_AGENT_INBOX_DIR=/mnt/data/agent-inbox
```

systemdサービスで標準以外の場所を使う場合は、サービスのdrop-inで
`AGENT_INBOX_DIR` と `ReadWritePaths` を同じ場所へ変更する必要があります。

すでに起動しているDev Containerへマウントを追加するには、一度再構築します。

```bash
dc rebuild
```

現在のDev Container CLIの `--mount` にはread-only指定がないため、
`/inbox` はコンテナからも書き込み可能です。Agent Inboxを使用しない場合や、
ホストディレクトリを書き込み可能でマウントしたくない場合は無効化できます。

```bash
export DC_AGENT_INBOX=0
```

`false`、`no`、`off` でも無効化できます。
無効化した状態で既存コンテナからマウントを外す場合も `dc rebuild` が必要です。

### Webアプリの制限

- 保存できる形式はPNG、JPEG、GIF、UTF-8プレーンテキスト
- 画像・テキストとも1ファイルの標準上限は20 MiB
- 画像は一辺20,000ピクセル、総ピクセル数1億まで
- アプリはユーザーが指定したファイル名を保存先に使用しない
- 削除できるのは保存ディレクトリ直下の対応ファイルだけ
- 保存領域の自動削除は行わないため、不要なファイルはWeb UIから手動で削除する

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
