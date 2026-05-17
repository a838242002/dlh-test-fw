# MySQL target

A throwaway single-pod MySQL 8 for scenario testing.

    kubectl apply -f targets/mysql/deploy.yaml
    kubectl -n mysql-sys rollout status deploy/mysql

Credentials: see Secret `mysql-creds` in namespace `mysql-sys`.
