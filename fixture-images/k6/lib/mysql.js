// MySQL primitives over xk6-sql. Also usable against Doris query API
// (Doris speaks MySQL protocol for queries).
//
// The Trend name `dlh_mysql_query_duration_seconds` is the basis for the
// dlh-mysql Grafana dashboard's p95 latency panel (Plan 8).

import sql from 'k6/x/sql';
import driver from 'k6/x/sql/driver/mysql';
import { Trend } from 'k6/metrics';
import { nowSec } from '/scripts/lib/common.js';

const queryDuration = new Trend('dlh_mysql_query_duration_seconds', true);

/** Open a MySQL connection. dsn shape: "user:pass@tcp(host:3306)/db". */
export function openConn(dsn) {
  return sql.open(driver, dsn);
}

/** Execute a write statement; records latency under op="exec". */
export function exec(db, statement, ...args) {
  const t0 = nowSec();
  try {
    return db.exec(statement, ...args);
  } finally {
    queryDuration.add(nowSec() - t0, { op: 'exec' });
  }
}

/** Run a read query; records latency under op="query". Returns rows. */
export function query(db, q, ...args) {
  const t0 = nowSec();
  try {
    return db.query(q, ...args);
  } finally {
    queryDuration.add(nowSec() - t0, { op: 'query' });
  }
}
