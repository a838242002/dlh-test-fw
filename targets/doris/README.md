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
