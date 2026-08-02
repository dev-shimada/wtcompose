# wtcompose

複数の git worktree で同じ `docker-compose.yml` を並行して起動できるように、
worktree ごとに一意な `container_name` / ホスト側ポートを割り当てた
`docker-compose.override.yml` を生成する CLI。

もともと zsh 関数 (`compose-worktree-override`, `yq` 依存) だったものを Go に移植したもの。
`yq` 不要、単一バイナリで動く。特定のリポジトリに依存せず、`docker-compose.yml` があるディレクトリならどこでも使える。

## 使い方

`docker-compose.yml` があるディレクトリで実行する。

```sh
cd path/to/worktree
wtcompose
```

- `container_name` または `ports` を持つ service について、`docker-compose.override.yml` を生成する
- `container_name` は `<repo>-<worktree>-<service>`(repo名とworktree名が同じ、つまりメインworktreeなら `<repo>-<service>`)
- `ports` のホスト側は `~/.config/compose-worktree-override/ports.tsv` に永続化された空きポート(20000〜)を割り当てる。同じ repo/worktree/service/元ポートの組み合わせなら再実行しても同じ値を再利用する
- 元の `docker-compose.yml` は変更しない
- 生成した `docker-compose.override.yml` は、git worktree 間で共有される `.git/info/exclude` に登録する(リポジトリ本体のファイルではなく worktree ローカルの設定なので、`git status` を汚さない)
- 対象 service が1つもない場合は何もせず、以前このツールが生成した override ファイルが残っていれば削除する
- このツールが生成したものではない `docker-compose.override.yml` が既に存在する場合は上書きしない

`docker compose up` はそのまま呼べば `docker-compose.override.yml` を自動的にマージする。

```sh
docker compose up
```

## インストール

```sh
go install github.com/dev-shimada/wtcompose@latest
```

もしくはこのディレクトリで:

```sh
make build   # ./wtcompose を生成
make install # $GOBIN に install
```

## 対応していないポート形式

`ports` のうち `"ホスト:コンテナ"` (`"ホスト:コンテナ/tcp"` / `"/udp"` 含む) の形式だけホスト側を付け替える。
以下はそのまま出力される(= ホスト側ポートの衝突は解消されない):

- bind アドレス付き (`"127.0.0.1:8080:80"`)
- ポート範囲 (`"8000-8010:8000-8010"`)
- コンテナ側のみ (`"80"`)
- long syntax (`target:`/`published:` を使うマッピング形式) — この場合はエラーで停止する

元の zsh 版と同じ制約。空きポートの判定も `ports.tsv` に記録済みのポートとの重複回避のみで、
実際に OS 上で bind 可能かはチェックしない。

## 開発

```sh
go test ./...
```
