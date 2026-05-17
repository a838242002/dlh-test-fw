// Doris primitives.
//
// Doris exposes two relevant APIs:
//   - Stream Load (HTTP PUT) for ingest  → use k6/http (no extension needed)
//   - MySQL protocol for queries         → reuse xk6-sql with the mysql driver
//
// Trend `dlh_doris_streamload_duration_seconds` (tagged db, table) plus
// counter `dlh_doris_streamload_rows_total` feed the dlh-doris dashboard.
// Query latency is reported under `dlh_doris_query_duration_seconds`.

import http from 'k6/http';
import sql from 'k6/x/sql';
import driver from 'k6/x/sql/driver/mysql';
import encoding from 'k6/encoding';
import { Trend, Counter } from 'k6/metrics';
import { nowSec } from '/scripts/lib/common.js';

const streamLoadDuration = new Trend('dlh_doris_streamload_duration_seconds', true);
const streamLoadRows = new Counter('dlh_doris_streamload_rows_total');
const queryDuration = new Trend('dlh_doris_query_duration_seconds', true);

/**
 * Send one Stream Load batch.
 *
 *   feHost, fePort       — Doris frontend
 *   db, table            — target
 *   user, pass           — Doris credentials
 *   columns              — comma-separated column list (e.g. "id, name, ts")
 *   rows                 — array of arrays; each inner array is one row in column order
 *
 * Returns the parsed JSON response from Doris (Status="Success" on happy path).
 * Records latency + rows-loaded metrics regardless of success.
 */
export function streamLoad({ feHost, fePort, db, table, user, pass, columns, rows }) {
  const url = `http://${feHost}:${fePort}/api/${db}/${table}/_stream_load`;
  // CSV body: one row per line, comma-separated.
  const body = rows.map((r) => r.map((v) => String(v)).join(',')).join('\n');
  const labelTags = { db: db, table: table };
  const t0 = nowSec();
  try {
    const res = http.put(url, body, {
      headers: {
        'Authorization': 'Basic ' + encoding.b64encode(`${user}:${pass}`),
        'Content-Type': 'text/plain',
        'format': 'csv',
        'column_separator': ',',
        'columns': columns,
        'Expect': '100-continue',
      },
      tags: labelTags,
    });
    streamLoadRows.add(rows.length, labelTags);
    return res.json();
  } finally {
    streamLoadDuration.add(nowSec() - t0, labelTags);
  }
}

/** Open a query connection (MySQL protocol). dsn: "user:pass@tcp(fe:9030)/db". */
export function openConn(dsn) {
  return sql.open(driver, dsn);
}

/** Run a read query; records latency. Returns rows. */
export function queryRows(db, q, ...args) {
  const t0 = nowSec();
  try {
    return db.query(q, ...args);
  } finally {
    queryDuration.add(nowSec() - t0);
  }
}
