FROM alpine:3.20
RUN apk add --no-cache curl bash ca-certificates wget \
 && ARCH=$(uname -m) && case "$ARCH" in x86_64) MCARCH=linux-amd64 ;; aarch64|arm64) MCARCH=linux-arm64 ;; *) MCARCH=linux-amd64 ;; esac \
 && wget -q https://dl.min.io/client/mc/release/${MCARCH}/mc -O /usr/local/bin/mc \
 && chmod +x /usr/local/bin/mc
WORKDIR /work
ENTRYPOINT ["/bin/bash", "-c"]
