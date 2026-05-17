// Generic MySQL runner. Drives mysql.js primitives based on env vars.
// One scenario YAML sets env_map; this script handles read/write/mixed
// patterns without per-scenario customization.
//
// Env interface (see spec §Generic runners / runners/mysql.js):
//   MYSQL_DSN          — required, "user:pass@tcp(host:3306)/db"
//   MYSQL_OP_MIX       — default "read:100", e.g. "read:70,write:30"
//   MYSQL_READ_SQL     — default "SELECT NOW()"
//   MYSQL_WRITE_SQL    — default "INSERT INTO dlh_load(ts) VALUES(NOW())"
//   MYSQL_SLEEP_MS     — default 0
//   WORKLOAD           — default "steady" (only mode in Phase 2)
//   SCENARIO_LABEL     — propagated as dlh_scenario tag
//   VUS                — overrides scenario YAML default
//   DURATION           — overrides scenario YAML default

import { sleep } from 'k6';
import { openConn, exec, query } from '/scripts/lib/mysql.js';
import { buildOptions, parseOpMix, errCounter } from '/scripts/lib/common.js';

const dsn = __ENV.MYSQL_DSN;
if (!dsn) {
  // k6 doesn't surface init-time throws nicely; fall back to a marker so
  // the test fails visibly rather than silently producing 0 metrics.
  throw new Error('MYSQL_DSN is required but unset');
}
const ops = parseOpMix(__ENV.MYSQL_OP_MIX || 'read:100');
const sqlR = __ENV.MYSQL_READ_SQL  || 'SELECT NOW()';
const sqlW = __ENV.MYSQL_WRITE_SQL || 'INSERT INTO dlh_load(ts) VALUES(NOW())';
const sleepMs = parseInt(__ENV.MYSQL_SLEEP_MS || '0', 10);

export const options = buildOptions({
  scenarioLabel: 'mysql',
  vus: 10,
  duration: '180s',
});

let db;
export function setup() {
  // setup() runs once before VUs spin up.
  db = openConn(dsn);
  return { dsn: dsn };  // returned object is passed to teardown(); also keeps it referenced
}

export default function () {
  if (!db) db = openConn(dsn);  // each VU runs default() in its own JS isolate, re-open
  const op = ops.pick();
  try {
    if (op === 'read') {
      query(db, sqlR);
    } else {
      exec(db, sqlW);
    }
  } catch (e) {
    errCounter.add(1, { kind: `mysql-${op}` });
  }
  if (sleepMs > 0) sleep(sleepMs / 1000);
}

export function teardown() {
  if (db) {
    try { db.close(); } catch (e) { /* shutdown — ignore */ }
  }
}
