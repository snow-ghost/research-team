import test from "node:test";
import assert from "node:assert/strict";
import { verificationCurrent } from "../src/proof-state.js";

test("проверка действительна только для текущей цели и закрепленной библиотеки", () => {
  const entity = { revision: 2 };
  const v = { target_revision: 2, library_pins: { module: "digest" } };
  const l = {
    id: "module",
    status: "ready",
    report: { artifact_sha256: "digest" },
  };
  assert.equal(verificationCurrent(v, entity, { library: [l] }), true);
  assert.equal(
    verificationCurrent(v, { revision: 3 }, { library: [l] }),
    false,
  );
  assert.equal(verificationCurrent(v, entity, { library: [] }), false);
  assert.equal(
    verificationCurrent(v, entity, { library: [{ ...l, status: "stale" }] }),
    false,
  );
  assert.equal(
    verificationCurrent(v, entity, {
      library: [{ ...l, report: { artifact_sha256: "different" } }],
    }),
    false,
  );
  assert.equal(verificationCurrent({ target_revision: 2 }, entity, {}), true);
});
