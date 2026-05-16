# dlh-test-fw

Umbrella Helm chart for the Chaos + Load Test Platform.

## Quick start (minikube)

    helm dependency update helm/dlh-test-fw
    helm upgrade --install dlh helm/dlh-test-fw \
      -n dlh-test-fw --create-namespace \
      -f helm/dlh-test-fw/values-minikube.yaml --wait

## Smoke test

    make platform-verify

## Uninstall

    make platform-down
