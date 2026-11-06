FROM node:25.9.0-bookworm-slim@sha256:81db02c4b671288a03915da9534dbd54f96d0e7c24d80ccc54f5b36b2e684370 AS node
FROM golang:1.25.6-bookworm@sha256:f4490d7b261d73af4543c46ac6597d7d101b6e1755bcdd8c5159fda7046b6b3e AS native
ENV GOTOOLCHAIN=local
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
COPY benchmark/go/profiling_benchmark.go.txt /tmp/profiling_benchmark.go
RUN mkdir /out && CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o /out/stow-s3 ./cmd/stow-s3 && go build -trimpath -o /out/pprof cmd/pprof
RUN cp /tmp/profiling_benchmark.go cmd/stow-s3/profiling_benchmark.go && CGO_ENABLED=0 go build -trimpath -o /out/stow-s3-profile ./cmd/stow-s3
FROM node:25.9.0-bookworm-slim@sha256:81db02c4b671288a03915da9534dbd54f96d0e7c24d80ccc54f5b36b2e684370
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates procps && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=native /out/ ./bin/
COPY packages/stow-s3/package.json packages/stow-s3/package-lock.json ./packages/stow-s3/
RUN npm ci --ignore-scripts --omit=optional --prefix packages/stow-s3
COPY packages/stow-s3/dist ./packages/stow-s3/dist
COPY packages/stow-s3/scripts/performance ./packages/stow-s3/scripts/performance
COPY benchmark ./benchmark
RUN node --test benchmark/test/*.test.mjs && node benchmark/diagnosis-identity.mjs
CMD ["node", "benchmark/target.mjs"]
