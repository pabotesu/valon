FROM quay.io/coreos/etcd:v3.5.17

# etcdデータディレクトリ
ENV ETCD_DATA_DIR=/etcd-data

# ボリュームマウントポイント
VOLUME ["/etcd-data"]

# クライアントAPI、ピア通信ポート
EXPOSE 2379 2380

# デフォルトのetcd起動コマンド
CMD ["/usr/local/bin/etcd", \
     "--name=valon-etcd", \
     "--data-dir=/etcd-data", \
     "--listen-client-urls=http://0.0.0.0:2379", \
     "--advertise-client-urls=http://localhost:2379", \
     "--listen-peer-urls=http://0.0.0.0:2380", \
     "--initial-advertise-peer-urls=http://localhost:2380", \
     "--initial-cluster=valon-etcd=http://localhost:2380", \
     "--initial-cluster-token=valon-cluster", \
     "--initial-cluster-state=new", \
     "--log-level=info"]
