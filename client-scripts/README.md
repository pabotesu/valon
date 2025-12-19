# VALON Client Scripts

クライアント側のシェルスクリプト群

## 構成

```
client-scripts/
├── bin/
│   ├── valon-peer-add     # Peer追加スクリプト
│   └── valon-sync         # エンドポイント同期(自分の情報更新＋他Peerの情報取得)
└── config_examples/
    ├── valon-sync.conf.example  # 設定ファイルの例
    └── peers.conf.example       # Peer定義の例
```

## インストール

### 最小インストール（推奨）

```bash
# スクリプトのみコピー
sudo cp bin/* /usr/local/bin/
sudo chmod +x /usr/local/bin/valon-*

# 設定ファイルはカレントディレクトリまたはホームディレクトリに配置
mkdir -p ~/.config/valon
cp config_examples/valon-sync.conf.example ~/.config/valon/client.conf
vi ~/.config/valon/client.conf  # 必要に応じて編集
```

### システム全体にインストール

```bash
# システムディレクトリにコピー
sudo mkdir -p /etc/valon
sudo cp config_examples/valon-sync.conf.example /etc/valon/client.conf
sudo cp bin/* /usr/local/bin/
sudo chmod +x /usr/local/bin/valon-*

# 設定ファイルを編集
sudo vi /etc/valon/client.conf
```

## 設定ファイルの探索順序

スクリプトは以下の順序で設定ファイルを探します：

1. 環境変数: `$VALON_CONFIG` または `$CONFIG_FILE`
2. コマンドライン引数: `--config <path>`
3. カレントディレクトリ: `./valon-sync.conf`
4. ユーザーホーム: `~/.config/valon/client.conf`
5. システム: `/etc/valon/client.conf`

設定ファイルが見つからない場合は、環境変数のみで動作します。

## 環境変数での設定

設定ファイルなしで環境変数のみで動作可能：

```bash
export VALON_INTERFACE=wg0
export VALON_WG_IP=100.100.0.2
export VALON_ALIAS=mylaptop
export VALON_API=http://100.100.0.1:8053
export VALON_DNS_ZONE=valon.internal

sudo -E valon-sync
```

## 使い方

### 初回セットアップ（最小構成）

```bash
# 1. WireGuardインターフェースを起動
# (valonctl peer addで生成された設定を/etc/wireguard/wg0.confに保存済みとする)
sudo wg-quick up wg0

# 2. 接続確認
ping -c 3 100.100.0.1
dig @100.100.0.1 discovery.valon.internal

# 3. エンドポイント同期（環境変数で設定）
export VALON_WG_IP=$(ip -4 addr show wg0 | grep -oP '(?<=inet\s)\d+(\.\d+){3}')
export VALON_ALIAS=mylaptop
sudo -E valon-sync
```

### 初回セットアップ（設定ファイル使用）

```bash
# 1. 設定ファイル作成
mkdir -p ~/.config/valon
cat > ~/.config/valon/client.conf << EOF
WG_INTERFACE=wg0
OWN_WG_IP=100.100.0.2
OWN_ALIAS=mylaptop
DISCOVERY_API=http://100.100.0.1:8053
DNS_ZONE=valon.internal
EOF

# 2. WireGuardインターフェースを起動
sudo wg-quick up wg0

# 3. 接続確認
ping -c 3 100.100.0.1

# 4. エンドポイント同期
sudo valon-sync
```

### 1. Peer追加（手動実行）

**重要**: Discovery Roleは既に`wg0.conf`で設定されているため、`peers.conf`には**他のピア（クライアント）のみ**を記載してください。

```bash
# peers.confに他のクライアントを定義
cat > peers.conf << EOF
PEER_NIXBOOK_PUBKEY=4YtYaSKa0CKze5VRnl70qOc6RqRvn6mZUflu5KJt5BU=
PEER_NIXBOOK_ALIAS=nix-book
PEER_NIXBOOK_ALLOWED_IPS=100.100.0.3/32
EOF

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

### systemd-timer で定期実行（環境変数版）

```bash
# /etc/systemd/system/valon-sync.service
[Unit]
Description=VALON Sync - Update own endpoint and resolve peer endpoints
After=network-online.target wg-quick@wg0.service

[Service]
Type=oneshot
Environment="VALON_INTERFACE=wg0"
Environment="VALON_API=http://100.100.0.1:8053"
ExecStart=/bin/bash -c 'export VALON_WG_IP=$(ip -4 addr show wg0 | grep -oP "(?<=inet\\s)\\d+(\\.\\d+){3}") && export VALON_ALIAS=$(hostname) && /usr/local/bin/valon-sync'
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

### systemd-timer で定期実行（設定ファイル版）

```bash
# /etc/systemd/system/valon-sync.service
[Unit]
Description=VALON Sync - Update own endpoint and resolve peer endpoints
After=network-online.target wg-quick@wg0.service

[Service]
Type=oneshot
ExecStart=/usr/local/bin/valon-sync --config /etc/valon/client.conf
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
