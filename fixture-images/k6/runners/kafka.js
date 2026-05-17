// Generic Kafka runner.
//
// Env interface (see spec §runners/kafka.js):
//   KAFKA_BOOTSTRAP      — required, "broker1:9092,broker2:9092"
//   KAFKA_TOPIC          — required
//   KAFKA_OP             — default "produce" — one of produce | consume | both
//   KAFKA_MESSAGE_SIZE   — default 256 (bytes per message; random-filled)
//   KAFKA_CONSUME_GROUP  — default "dlh-test-fw"
//   VUS, DURATION, SCENARIO_LABEL — same as mysql runner

import { newWriter, newReader, produce, readN } from '/scripts/lib/kafka.js';
import { buildOptions, errCounter } from '/scripts/lib/common.js';

const brokers = __ENV.KAFKA_BOOTSTRAP;
const topic = __ENV.KAFKA_TOPIC;
if (!brokers) throw new Error('KAFKA_BOOTSTRAP is required but unset');
if (!topic)   throw new Error('KAFKA_TOPIC is required but unset');
const op = __ENV.KAFKA_OP || 'produce';
const messageSize = parseInt(__ENV.KAFKA_MESSAGE_SIZE || '256', 10);
const group = __ENV.KAFKA_CONSUME_GROUP || 'dlh-test-fw';

export const options = buildOptions({
  scenarioLabel: 'kafka',
  vus: 10,
  duration: '180s',
});

let writer;
let reader;
export function setup() {
  if (op === 'produce' || op === 'both') writer = newWriter(brokers, topic);
  if (op === 'consume' || op === 'both') reader = newReader(brokers, topic, group);
  return {};
}

function makePayload() {
  // Deterministic per-VU/per-iter content of approximately messageSize bytes.
  const filler = 'x'.repeat(Math.max(0, messageSize - 16));
  return `${__VU}-${__ITER}-${filler}`;
}

export default function () {
  if (op === 'produce' || op === 'both') {
    if (!writer) writer = newWriter(brokers, topic);
    try {
      produce(writer, topic, [{ value: makePayload() }]);
    } catch (e) {
      errCounter.add(1, { kind: 'kafka-produce' });
    }
  }
  if (op === 'consume' || op === 'both') {
    if (!reader) reader = newReader(brokers, topic, group);
    try {
      readN(reader, 1);
    } catch (e) {
      errCounter.add(1, { kind: 'kafka-consume' });
    }
  }
}

export function teardown() {
  try { if (writer) writer.close(); } catch (e) { /* shutdown */ }
  try { if (reader) reader.close(); } catch (e) { /* shutdown */ }
}
