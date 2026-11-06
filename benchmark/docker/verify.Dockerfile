FROM node:25.9.0-bookworm-slim@sha256:81db02c4b671288a03915da9534dbd54f96d0e7c24d80ccc54f5b36b2e684370 AS node
FROM golang:1.25.6-bookworm@sha256:f4490d7b261d73af4543c46ac6597d7d101b6e1755bcdd8c5159fda7046b6b3e AS verify
ENV GOTOOLCHAIN=local BENCH_CLOUD_VERIFY=1 GODEBUG=netdns=cgo
COPY --from=node /usr/local /usr/local
RUN apt-get update && apt-get install -y --no-install-recommends python3 make git protobuf-compiler libnss-myhostname chromium && rm -rf /var/lib/apt/lists/*
RUN sed -i 's/^hosts:.*/hosts: files myhostname dns/' /etc/nsswitch.conf && getent ahostsv4 endpoint.localhost && getent ahostsv4 photos.endpoint.localhost
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
COPY . .
RUN PATH=/go/bin:$PATH protoc --go_out=/tmp --go_opt=paths=source_relative internal/storage/fs/recordpb/record.proto && cmp /tmp/internal/storage/fs/recordpb/record.pb.go internal/storage/fs/recordpb/record.pb.go
RUN mkdir -p /out/verification /tmp/benchmark-go-cache /tmp/benchmark-npm && chown -R nobody:nogroup /src /out /tmp/benchmark-go-cache /tmp/benchmark-npm /go/pkg/mod
ENV GOCACHE=/tmp/benchmark-go-cache NPM_CONFIG_CACHE=/tmp/benchmark-npm PLAYWRIGHT_BROWSERS_PATH=/tmp/benchmark-playwright
RUN bash -o pipefail -c "su nobody -s /bin/sh -c 'node benchmark/verify.mjs' 2>&1 | tee /out/verification/standards.log"
FROM node:25.9.0-bookworm-slim@sha256:81db02c4b671288a03915da9534dbd54f96d0e7c24d80ccc54f5b36b2e684370
WORKDIR /app
COPY --from=verify /out/verification /app/verification
COPY benchmark ./benchmark
CMD ["node", "benchmark/runner.mjs"]
