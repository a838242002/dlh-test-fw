# Doris target — SKIPPED on this workstation

Phase 1 deliberately ships Doris as optional. The apache/doris 2.x FE+BE pair
is memory-heavy (~4-5 GiB combined) and historically flaky on arm64 minikube
(amd64 emulation, BE init crash loops, edit-log races on single-node FE).

The scenario file `scenarios/doris-be-network-loss.yaml` is still committed
as documentation of the pattern; running it requires a Doris install per
plan §Task 3 Step 2. To revive:

1. Apply the FE+BE StatefulSets from Plan 5 §Task 3 Step 2.
2. Wait for the FE leader to register the BE (`SHOW BACKENDS`).
3. Create a `doris-creds` Secret with stream-load credentials referenced by
   `fixture-minio-load-doris`.
4. Seed a fixture CSV into MinIO at `s3://fixtures/doris-rows.csv`.
5. `kubectl apply -f scenarios/doris-be-network-loss.yaml`.

The scenario is marked **deferred** in `scenarios/README.md` until then.

## Plan 7 spike attempt (2026-05-17): NO-GO

Tried `apache/doris:doris-all-in-one-2.1.7` (manifest not found) and
fell back to `apache/doris:2.1.9-all` (arm64 multi-arch confirmed).
Image pulled (6.9 GiB) but the all-in-one entrypoint exits cleanly
after BE registration to FE, causing CrashLoopBackOff; container also
emits a `vm.max_map_count` sysctl warning (minikube default ~262144,
Doris BE wants 2,000,000), and even on a single startup the FE shows
"System has no available disk capacity or no available BE nodes" —
the BE never reports `Alive: true`. `SHOW BACKENDS` from a mysql client
returned `Can't connect to MySQL server` because the pod was looping.

Consequence for Phase 2: `scenarios/doris-be-network-loss.yaml` is NOT
migrated to a real-target shape; it remains the Phase 1 stub. Plan 8
ships `dlh-doris` dashboard with the same panel layout as the others
but no live data path until a future phase brings Doris up (likely via
the separate `apache/doris.fe-ubuntu` + `apache/doris.be-ubuntu` images
on a VM with `vm.max_map_count` tunable, not minikube).
