FROM node:25.9.0-bookworm-slim@sha256:81db02c4b671288a03915da9534dbd54f96d0e7c24d80ccc54f5b36b2e684370 AS node
FROM golang:1.25.6-bookworm@sha256:f4490d7b261d73af4543c46ac6597d7d101b6e1755bcdd8c5159fda7046b6b3e AS check
COPY --from=node /usr/local /usr/local
ENV GOTOOLCHAIN=local BENCH_CLOUD_VERIFY=1
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN mkdir -p /out/verification /tmp/go-cache && chown -R nobody:nogroup /src /out /tmp/go-cache /go/pkg/mod
ENV GOCACHE=/tmp/go-cache
RUN bash -o pipefail -c "su nobody -s /bin/sh -c 'node benchmark/read-performance.mjs' 2>&1 | tee /out/verification/read.log"
FROM node:25.9.0-bookworm-slim@sha256:81db02c4b671288a03915da9534dbd54f96d0e7c24d80ccc54f5b36b2e684370
WORKDIR /app
COPY --from=check /out/verification ./verification
COPY benchmark ./benchmark
CMD ["node","benchmark/runner.mjs"]
