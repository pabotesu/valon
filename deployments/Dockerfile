FROM quay.io/coreos/etcd:v3.5.17

# ボリュームマウントポイント
VOLUME ["/etcd-data"]

# クライアントAPI、ピア通信ポート
EXPOSE 2379 2380

# デフォルトのetcd起動コマンド（シングルノード構成）
# listen: 0.0.0.0で全インターフェースをリッスン
# advertise: 127.0.0.1で同一ホスト内のクライアントからアクセス可能
CMD ["/usr/local/bin/etcd", \
     "--name=valon-etcd", \
     "--data-dir=/etcd-data", \
     "--listen-client-urls=http://0.0.0.0:2379", \
     "--advertise-client-urls=http://127.0.0.1:2379", \
     "--listen-peer-urls=http://0.0.0.0:2380", \
     "--initial-advertise-peer-urls=http://127.0.0.1:2380", \
     "--initial-cluster=valon-etcd=http://127.0.0.1:2380", \
     "--initial-cluster-token=valon-cluster", \
     "--initial-cluster-state=new", \
     "--log-level=info"]
