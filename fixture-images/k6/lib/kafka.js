// Kafka primitives over xk6-kafka.
//
// Trend `dlh_kafka_produce_duration_seconds` (tagged with `topic`) + counter
// `dlh_kafka_messages_produced_total` feed the dlh-kafka dashboard (Plan 8).
//
// xk6-kafka's API surface in v0.27.x: Writer({brokers, topic, ...}), Reader({brokers, topic, groupID, ...}).

import { Writer, Reader, SchemaRegistry, SCHEMA_TYPE_STRING } from 'k6/x/kafka';
import { Trend, Counter } from 'k6/metrics';
import { nowSec } from '/scripts/lib/common.js';

const produceDuration = new Trend('dlh_kafka_produce_duration_seconds', true);
const messagesProduced = new Counter('dlh_kafka_messages_produced_total');
const sr = new SchemaRegistry();

/** Build a Writer. brokers is comma-separated "host:port". */
export function newWriter(brokers, topic) {
  return new Writer({
    brokers: brokers.split(',').map((s) => s.trim()),
    topic: topic,
    autoCreateTopic: true,
  });
}

/**
 * Produce one batch of messages.
 *
 *   writer: from newWriter()
 *   topic:  for tagging metrics (not strictly needed by the Writer, but used here for labels)
 *   messages: array of { key?: string, value: string }
 */
export function produce(writer, topic, messages) {
  const wire = messages.map((m) => ({
    key:   m.key   != null ? sr.serialize({ data: m.key,   schemaType: SCHEMA_TYPE_STRING }) : null,
    value: sr.serialize({ data: String(m.value), schemaType: SCHEMA_TYPE_STRING }),
  }));
  const t0 = nowSec();
  try {
    writer.produce({ messages: wire });
    messagesProduced.add(messages.length, { topic: topic });
  } finally {
    produceDuration.add(nowSec() - t0, { topic: topic });
  }
}

/** Build a Reader. */
export function newReader(brokers, topic, groupID) {
  return new Reader({
    brokers: brokers.split(',').map((s) => s.trim()),
    topic: topic,
    groupID: groupID,
  });
}

/** Read up to `limit` messages. Returns the array of consumed messages. */
export function readN(reader, limit) {
  return reader.consume({ limit: limit });
}
