# VALON Deployments

etcdコンテナのデプロイ設定です。

## 前提条件

- Podman installed

```bash
# Podman インストール確認
podman --version
```

## 使用方法

### etcd起動

```bash
cd deployments

# イメージビルド
podman build -t valon-etcd:latest .

# コンテナ起動（ボリュームは自動作成される）
podman run -d \
  --name valon-etcd \
  -p 2379:2379 \
  -p 2380:2380 \
  -v valon-etcd-data:/etcd-data \
  valon-etcd:latest
```

**Docker使用の場合:** `podman`を`docker`に置き換えてください。

### 状態確認

```bash
# コンテナ状態確認
podman ps

# etcd health check
podman exec valon-etcd etcdctl endpoint health

# ログ確認
podman logs valon-etcd
```

### etcd停止

```bash
# コンテナ停止・削除
podman stop valon-etcd
podman rm valon-etcd

# データボリューム削除も含める場合
podman volume rm valon-etcd-data
```

## etcd 接続情報

- Client API: `http://127.0.0.1:2379`
- Peer communication: `http://127.0.0.1:2380`

CoreDNS VALONプラグインの設定例:
```
valon.internal:53 {
    valon {
        etcd_endpoints http://127.0.0.1:2379
        wg_interface wg0
        ddns_listen 127.0.0.1:8080
    }
}
```

## データ永続化

etcdのデータは名前付きボリューム `valon-etcd-data` に保存されます。

```bash
# ボリューム確認
podman volume ls

# ボリューム詳細
podman volume inspect valon-etcd-data
```

## トラブルシューティング

### ポート競合
既にポート2379/2380が使用されている場合:
```bash
# ポート使用確認
lsof -i :2379
lsof -i :2380
```

### コンテナログ確認
```bash
podman logs -f valon-etcd
```

### etcdctl直接実行
```bash
podman exec -it valon-etcd etcdctl \
  --endpoints=http://localhost:2379 \
  member list
```
