// Image build smoke.
//
// Imports every xk6 module surface to fail-fast if a plugin didn't link,
// and every lib file to fail-fast if any file has a syntax error or a bad
// import path. Doesn't connect anywhere; runs a single 1-VU 1s iteration.
//
// Invocation:
//   docker run --rm dlh-k6:0.1.0 run /scripts/lib/smoke.js

import sql from 'k6/x/sql';                            // xk6-sql linked?
import mysqlDriver from 'k6/x/sql/driver/mysql';       // xk6-sql-driver-mysql linked?
import { Writer } from 'k6/x/kafka';                   // xk6-kafka linked?

import * as common from '/scripts/lib/common.js';
import * as mysqlLib from '/scripts/lib/mysql.js';
import * as kafkaLib from '/scripts/lib/kafka.js';
import * as dorisLib from '/scripts/lib/doris.js';

export const options = {
  vus: 1,
  iterations: 1,
};

export default function () {
  // Reference each import so a tree-shaker (k6 doesn't have one today, but for
  // future-proofing and clarity) doesn't drop the symbol.
  if (!sql || !mysqlDriver || !Writer) {
    throw new Error('xk6 modules missing — image build is broken');
  }
  if (!common.buildOptions || !mysqlLib.openConn || !kafkaLib.newWriter || !dorisLib.streamLoad) {
    throw new Error('lib file missing expected export — JS sources are broken');
  }
  // Exercise one parseOpMix call so the helper is covered too.
  const picker = common.parseOpMix('a:1,b:1');
  if (!['a', 'b'].includes(picker.pick())) {
    throw new Error('parseOpMix is broken');
  }
}
