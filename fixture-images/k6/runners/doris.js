// Generic Doris runner.
//
// Env interface (see spec §runners/doris.js):
//   DORIS_FE_HOST / DORIS_FE_PORT  — required (port defaults to 8030 for Stream Load,
//                                     9030 for MySQL query)
//   DORIS_DB / DORIS_TABLE         — required
//   DORIS_USER / DORIS_PASS        — default root / "" (empty pass)
//   DORIS_OP                       — default "stream_load" — one of stream_load|query|both
//   DORIS_BATCH_ROWS               — default 1000 (rows per Stream Load batch)
//   DORIS_QUERY_SQL                — default "SELECT COUNT(*) FROM <table>"
//   VUS, DURATION, SCENARIO_LABEL  — same as mysql runner

import { streamLoad, openConn, queryRows } from '/scripts/lib/doris.js';
import { buildOptions, errCounter } from '/scripts/lib/common.js';

const feHost = __ENV.DORIS_FE_HOST;
const fePort = __ENV.DORIS_FE_PORT || '8030';
const db = __ENV.DORIS_DB;
const table = __ENV.DORIS_TABLE;
if (!feHost || !db || !table) throw new Error('DORIS_FE_HOST, DORIS_DB, DORIS_TABLE are required');
const user = __ENV.DORIS_USER || 'root';
const pass = __ENV.DORIS_PASS || '';
const op = __ENV.DORIS_OP || 'stream_load';
const batchRows = parseInt(__ENV.DORIS_BATCH_ROWS || '1000', 10);
const querySql = __ENV.DORIS_QUERY_SQL || `SELECT COUNT(*) FROM ${table}`;

export const options = buildOptions({
  scenarioLabel: 'doris',
  vus: 10,
  duration: '180s',
});

let queryConn;
export function setup() {
  if (op === 'query' || op === 'both') {
    // Doris query port is 9030, not 8030.
    const queryPort = __ENV.DORIS_QUERY_PORT || '9030';
    queryConn = openConn(`${user}:${pass}@tcp(${feHost}:${queryPort})/${db}`);
  }
  return {};
}

function makeBatch(n) {
  // Two-column table assumption: (id BIGINT, ts DATETIME). Override via
  // DORIS_TABLE to point at a table matching this shape, or extend later.
  const rows = [];
  const now = new Date().toISOString().replace('T', ' ').substring(0, 19);
  for (let i = 0; i < n; i++) {
    rows.push([`${__VU}${__ITER}${i}`, now]);
  }
  return { columns: 'id, ts', rows };
}

export default function () {
  if (op === 'stream_load' || op === 'both') {
    const { columns, rows } = makeBatch(batchRows);
    try {
      const res = streamLoad({ feHost, fePort, db, table, user, pass, columns, rows });
      if (res && res.Status && res.Status !== 'Success') {
        errCounter.add(1, { kind: 'doris-streamload' });
      }
    } catch (e) {
      errCounter.add(1, { kind: 'doris-streamload' });
    }
  }
  if (op === 'query' || op === 'both') {
    if (!queryConn) {
      const queryPort = __ENV.DORIS_QUERY_PORT || '9030';
      queryConn = openConn(`${user}:${pass}@tcp(${feHost}:${queryPort})/${db}`);
    }
    try {
      queryRows(queryConn, querySql);
    } catch (e) {
      errCounter.add(1, { kind: 'doris-query' });
    }
  }
}

export function teardown() {
  try { if (queryConn) queryConn.close(); } catch (e) { /* shutdown */ }
}
