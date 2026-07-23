# devcontainer-user-env

Dev Containerをターミナル中心で利用するための個人用環境です。

次の機能を提供します。

- `dc bash` で現在のリポジトリのDev Containerに入る
- `dc codex` や `dc <command>` でDev Container内のツールを実行する
- `[HOST]` と `[DEV]` を明確に区別するシェルプロンプト
- Dev Container CLIのdotfiles機能を利用したインストール

開発対象のリポジトリに個人固有の変更を加える必要はありません。

## 必要なもの

- Bash
- Git
- [Dev Container CLI](https://github.com/devcontainers/cli)
- Docker、またはDev Container CLIが対応するコンテナランタイム
- [Starship](https://starship.rs/)（任意）

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
2. Bashのプロンプトに赤い `[HOST]` バッジを表示する

## 使い方

Dev Container設定を含むリポジトリで実行します。

```bash
dc
dc bash
dc codex
dc npm test
```

`dc codex` で起動したCodexには、`HERDR_AGENT=codex` が環境変数として
設定されます。

`dc` は次のようにコンテナを起動します。

```bash
devcontainer up \
  --workspace-folder "$PWD" \
  --dotfiles-repository <このリポジトリのURL> \
  --dotfiles-install-command install-devcontainer.sh
```

その後、指定されたコマンドを `devcontainer exec` で実行します。

dotfilesのGit URLには、このリポジトリの `origin` が自動的に使われます。
必要であれば環境変数で上書きできます。

```bash
export DC_DOTFILES_REPOSITORY=https://github.com/example/devcontainer-user-env.git
```

別のワークスペースを指定することもできます。

```bash
DC_WORKSPACE_FOLDER=/path/to/project dc bash
```

## プロンプトの色

- ホスト：赤背景に白文字の `[HOST]` バッジ
- Dev Container：青背景に白文字の `[DEV]` バッジ

DEVプロンプトは `/.dockerenv` も判定します。そのため、コンテナ内で新たに
対話シェルを起動した場合も `[DEV]` バッジが表示されます。

Starshipがインストールされている場合は、同梱の `starship.toml` を自動的に
使います。Starshipがない場合はBashの `PS1` に直接バッジを追加します。
