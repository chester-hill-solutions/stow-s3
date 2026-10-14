FROM node:25.9.0-bookworm-slim@sha256:81db02c4b671288a03915da9534dbd54f96d0e7c24d80ccc54f5b36b2e684370 AS node
FROM golang:1.25.6-bookworm@sha256:f4490d7b261d73af4543c46ac6597d7d101b6e1755bcdd8c5159fda7046b6b3e AS native
COPY --from=node /usr/local /usr/local
ENV GOTOOLCHAIN=local BENCH_CLOUD_AB=1
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
COPY benchmark ./benchmark
RUN mkdir -p /out /tmp/benchmark-go-cache && chown -R nobody:nogroup /src /out /go/pkg/mod /tmp/benchmark-go-cache
ENV GOCACHE=/tmp/benchmark-go-cache GOPATH=/go
RUN su nobody -s /bin/sh -c 'node benchmark/build-variants.mjs'
FROM node:25.9.0-bookworm-slim@sha256:81db02c4b671288a03915da9534dbd54f96d0e7c24d80ccc54f5b36b2e684370
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates procps && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=native /out/stow-s3 /out/stow-s3-before /out/embedded.test /out/embedded-before.test ./bin/
COPY --from=native /out/build-variants.json /out/benchmark-go.sum /out/baseline-regression.txt ./generated/
COPY packages/stow-s3/package.json packages/stow-s3/package-lock.json ./packages/stow-s3/
RUN npm ci --ignore-scripts --omit=optional --prefix packages/stow-s3
COPY packages/stow-s3/dist ./packages/stow-s3/dist
COPY packages/stow-s3/scripts/performance ./packages/stow-s3/scripts/performance
COPY benchmark ./benchmark
RUN node --test benchmark/test/*.test.mjs
RUN node benchmark/identity.mjs
CMD ["node", "benchmark/target.mjs"]
