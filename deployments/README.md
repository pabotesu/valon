# VALON Deployments

Discovery Role (etcd + CoreDNS) のコンテナデプロイ設定です。

## 構成

```
deployments/
├── docker-compose.yml       # etcd + CoreDNS統合構成
├── Dockerfile.etcd          # etcdコンテナ
└── Dockerfile.coredns       # CoreDNS + VALONプラグイン
```

## 前提条件

- Podman/Docker installed
- WireGuard interface (wg0) が起動済み ← **重要**

```bash
# Podman インストール確認
podman --version

# WireGuard確認
ip addr show wg0
```

## 使用方法

### 起動

```bash
cd deployments

# ビルド＋起動（初回はビルドに数分かかります）
sudo podman-compose up -d --build

# または Dockerの場合
sudo docker-compose up -d --build

# ログ確認
sudo podman-compose logs -f

# 特定サービスのログ
sudo podman-compose logs -f coredns
```

### 状態確認

```bash
# コンテナ状態
sudo podman ps

# etcd health check
sudo podman exec valon-etcd etcdctl endpoint health

# CoreDNSログ
sudo podman logs valon-coredns

# プラグイン確認（CoreDNS起動後）
dig @100.100.0.1 -p 53 example.valon.internal
```

### 停止

```bash
# コンテナ停止
sudo podman-compose down

# データボリューム削除も含める場合
sudo podman-compose down -v
```

## 接続情報

### etcd
- Client API: `http://127.0.0.1:2379` (ホストから)
- データ保存: 名前付きボリューム `etcd-data`

### CoreDNS
- DNS: `100.100.0.1:53` (WireGuard IPでリッスン)
- DDNS API: `http://100.100.0.1:8053`
- 動作モード: `network_mode: host` (ホストのwg0を共有)

## 設定ファイル

### Corefile (`configs/Corefile.example`)
```
valon.internal:53 {
    valon {
        etcd_endpoints http://127.0.0.1:2379
        wg_interface wg0
        ddns_listen 100.100.0.1:8053
        wg_poll_interval 1s
        etcd_sync_interval 10s
    }
    cache 2
    log
    errors
}
```

## トラブルシューティング

### CoreDNSが起動しない

**原因**: WireGuardインターフェースが起動していない

```bash
# wg0確認
ip addr show wg0

# wg0起動
sudo wg-quick up wg0

# CoreDNS再起動
sudo podman-compose restart coredns
```

### etcdに接続できない

```bash
# etcdコンテナ状態確認
sudo podman logs valon-etcd

# ポート確認
sudo lsof -i :2379

# healthcheck
sudo podman exec valon-etcd etcdctl endpoint health
```

### DDNS APIにアクセスできない

```bash
# CoreDNSログ確認
sudo podman logs valon-coredns | grep "DDNS API"

# WireGuard IP確認
ip addr show wg0 | grep "100.100.0.1"

# curlテスト（WireGuardネットワーク内から）
curl http://100.100.0.1:8053/health
```

### ビルドエラー

```bash
# キャッシュをクリアして再ビルド
sudo podman-compose build --no-cache

# 個別ビルド確認
sudo podman build -f Dockerfile.coredns -t test-coredns ..
```

## データ永続化

etcdのデータは自動的に永続化されます：

```bash
# ボリューム確認
sudo podman volume ls | grep etcd-data

# ボリューム詳細
sudo podman volume inspect deployments_etcd-data

# バックアップ
sudo podman exec valon-etcd etcdctl snapshot save /etcd-data/backup.db
```

## 開発・デバッグ

```bash
# etcdコンテナに入る
sudo podman exec -it valon-etcd sh

# CoreDNSコンテナに入る
sudo podman exec -it valon-coredns bash

# etcdデータ確認
sudo podman exec valon-etcd etcdctl get --prefix /valon/

# WireGuard状態確認（CoreDNSコンテナ内）
sudo podman exec valon-coredns ip addr show wg0
```
