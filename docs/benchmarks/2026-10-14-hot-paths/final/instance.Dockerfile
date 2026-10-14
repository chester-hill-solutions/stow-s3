FROM node:25.9.0-bookworm-slim@sha256:81db02c4b671288a03915da9534dbd54f96d0e7c24d80ccc54f5b36b2e684370 AS node
FROM golang:1.25.6-bookworm@sha256:f4490d7b261d73af4543c46ac6597d7d101b6e1755bcdd8c5159fda7046b6b3e
COPY --from=node /usr/local /usr/local
RUN apt-get update && apt-get install -y --no-install-recommends python3 make git protobuf-compiler libnss-myhostname chromium && rm -rf /var/lib/apt/lists/*
RUN sed -i 's/^hosts:.*/hosts: files myhostname dns/' /etc/nsswitch.conf && getent ahostsv4 endpoint.localhost && getent ahostsv4 photos.endpoint.localhost
ENV GOTOOLCHAIN=local BENCH_CLOUD_VERIFY=1 BENCH_HOT_PATHS=1 GODEBUG=netdns=cgo GOCACHE=/tmp/benchmark-go-cache NPM_CONFIG_CACHE=/tmp/benchmark-npm PLAYWRIGHT_BROWSERS_PATH=/tmp/benchmark-playwright
WORKDIR /app
COPY benchmark ./benchmark
CMD ["node","benchmark/runner.mjs"]
