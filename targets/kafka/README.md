# Kafka target

A single-broker Kafka 3.7 (KRaft mode) for scenario testing.

    kubectl apply -f targets/kafka/deploy.yaml
    kubectl -n kafka-sys rollout status statefulset/kafka --timeout=240s

Bootstrap address: `kafka.kafka-sys.svc.cluster.local:9092`.

Pod labels include `kafka.broker.id: "0"` so `chaos-kafka-broker-partition` can
select it by `broker_id`.
