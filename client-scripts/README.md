# VALON Client Scripts

クライアント側のシェルスクリプト群

## 構成

```
client-scripts/
├── bin/
│   ├── valon-peer-add     # Peer追加スクリプト
│   └── valon-sync         # エンドポイント同期(自分の情報更新＋他Peerの情報取得)
└── etc/valon/
    ├── client.conf.example  # クライアント設定
    └── peers.conf.example   # Peer定義
```

## インストール

```bash
# コピー
sudo cp -r etc/valon /etc/
sudo cp bin/* /usr/local/bin/
sudo chmod +x /usr/local/bin/valon-*

# 設定ファイルを編集
sudo cp /etc/valon/client.conf.example /etc/valon/client.conf
sudo cp /etc/valon/peers.conf.example /etc/valon/peers.conf
sudo vi /etc/valon/client.conf  # OWN_WG_IP, OWN_ALIAS を設定
sudo vi /etc/valon/peers.conf   # 接続したいPeerを追加
```

## 使い方

### 初回セットアップ

```bash
# 1. Discovery Role側でPeerを追加し、WireGuard設定を取得
# (valonctl peer add <pubkey> --alias client01 の出力)

# 2. WireGuard設定ファイルを作成
sudo vi /etc/wireguard/wg0.conf
# → valonctlの出力をコピー
# → PrivateKeyを挿入

# 3. WireGuardインターフェースを起動
sudo wg-quick up wg0

# 4. 接続確認
ping -c 3 100.100.0.1
dig @100.100.0.1 discovery.valon.internal

# 5. 他のPeerを追加(必要なら peers.conf を編集)
sudo vi /etc/valon/peers.conf
sudo valon-peer-add

# 6. 初回同期実行
sudo valon-sync
```

### 1. Peer追加（手動実行）

```bash
# peers.confに定義したPeerをWireGuardに追加
sudo valon-peer-add
```

### 2. エンドポイント同期（定期実行推奨）

```bash
# 自分のLANエンドポイントを更新 + 全Peerのエンドポイントを解決して最適なものを設定
sudo valon-sync

# 特定のPeerだけ処理
sudo valon-sync --peer <pubkey>
```

**注意**: クライアントの初期登録はDiscovery Role側から行います。クライアント側から登録する機能はありません。

## 自動化

### systemd-timer で定期実行

```bash
# /etc/systemd/system/valon-sync.service
[Unit]
Description=VALON Sync - Update own endpoint and resolve peer endpoints
After=network-online.target wg-quick@wg0.service

[Service]
Type=oneshot
ExecStart=/usr/local/bin/valon-sync
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

```bash
# /etc/systemd/system/valon-sync.timer
[Unit]
Description=VALON Sync Timer
Requires=valon-sync.service

[Timer]
OnBootSec=30s
OnUnitActiveSec=60s
Unit=valon-sync.service

[Install]
WantedBy=timers.target
```

```bash
# 有効化
sudo systemctl daemon-reload
sudo systemctl enable valon-sync.timer
sudo systemctl start valon-sync.timer
```

## 動作フロー

### 初回セットアップ時
1. **Register**: Discovery Role側の管理コマンドでこのクライアントを登録
2. **WireGuard**: valonctlの出力をもとに `/etc/wireguard/wg0.conf` を作成し `wg-quick up`
3. **Add Peers**: `valon-peer-add` が他のPeerをWireGuardに追加
4. **Sync**: `valon-sync` が初回同期を実行

### 通常運用時
1. **定期的**: `valon-sync` (timer) が定期実行(60秒間隔推奨)
   - 自分のLANエンドポイントをDiscoveryに更新
   - 全PeerのDNS-SDでLAN/NATエンドポイントを取得
   - Ping試行(LAN優先、NAT fallback)
   - 最適なエンドポイントをWireGuardに設定

## トラブルシューティング

```bash
# ログ確認
sudo journalctl -u valon-sync.service -f

# 手動実行でデバッグ
sudo valon-sync
sudo valon-sync --peer <pubkey>  # 特定Peerのみ
```
