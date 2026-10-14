FROM node:25.9.0-bookworm-slim@sha256:81db02c4b671288a03915da9534dbd54f96d0e7c24d80ccc54f5b36b2e684370 AS node
FROM golang:1.25.6-bookworm@sha256:f4490d7b261d73af4543c46ac6597d7d101b6e1755bcdd8c5159fda7046b6b3e AS probe
ENV GOTOOLCHAIN=local BENCH_CLOUD_VERIFY=1 BENCH_WASM_NETWORK_PROBE=1 BINARYEN_CORES=2
COPY --from=node /usr/local /usr/local
RUN apt-get update && apt-get install -y --no-install-recommends binaryen && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN mkdir -p /out/verification && node benchmark/wasm-size-probe.mjs
FROM node:25.9.0-bookworm-slim@sha256:81db02c4b671288a03915da9534dbd54f96d0e7c24d80ccc54f5b36b2e684370
WORKDIR /app
COPY --from=probe /out/verification ./verification
COPY benchmark ./benchmark
CMD ["node", "benchmark/runner.mjs"]
