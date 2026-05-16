# Spike: k6 → VictoriaMetrics remote-write

Validates the Day-1 risk in the platform design: that k6 (run by k6-operator)
can push metrics into a single-node VictoriaMetrics via Prometheus remote-write,
and that those metrics are queryable filtered by a `dlh_scenario` label
(we namespace our label because `scenario` is reserved by k6).

## Run

    make up       # boot minikube, install charts, apply manifests, run k6
    make verify   # poll VM API; exits 0 iff success criterion is met
    make down     # tear down minikube cluster

## Success criterion

The PromQL query

    sum(k6_http_reqs_total{dlh_scenario="spike-httpbin"})

returns a value > 0 within 120 seconds of `make up` completing.

## Warning

`make up` will run `minikube delete` if a cluster already exists. Do not run
this on a workstation hosting other minikube clusters you care about.
