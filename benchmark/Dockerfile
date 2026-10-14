FROM golang:1.25.6-bookworm@sha256:f4490d7b261d73af4543c46ac6597d7d101b6e1755bcdd8c5159fda7046b6b3e AS native
ENV GOTOOLCHAIN=local CGO_ENABLED=0
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
COPY benchmark/go ./benchmark/go
RUN mkdir -p /tmp/benchmark-go-cache && chmod 1777 /tmp/benchmark-go-cache && su nobody -s /bin/sh -c 'CGO_ENABLED=1 GOCACHE=/tmp/benchmark-go-cache GOPATH=/go go test -race ./internal/storage/... ./internal/runtime/... ./pkg/stow/...'
RUN go build -trimpath -ldflags "-s -w" -o /out/stow-s3 ./cmd/stow-s3
RUN cd benchmark/go && go mod download && CGO_ENABLED=1 go test -race ./... && CGO_ENABLED=1 go test -run '^$' -bench 'BenchmarkComparison/.*/.*/4096B$' -benchtime=1x -count=1 -cpu=1,2 .
RUN cd benchmark/go && CGO_ENABLED=1 go test -run '^$' -bench '^BenchmarkAgentStartup$' -benchtime=2x -count=1 -cpu=1 .
RUN cd benchmark/go && CGO_ENABLED=1 go test -c -o /out/embedded.test . && cp go.sum /out/benchmark-go.sum

FROM native AS profiling
RUN go build -o /out/pprof cmd/pprof
RUN go test -c -o /out/fs.test ./internal/storage/fs && go test -c -o /out/runtime.test ./internal/runtime
RUN apt-get update && apt-get install -y --no-install-recommends protobuf-compiler && rm -rf /var/lib/apt/lists/*
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
RUN PATH=/go/bin:$PATH protoc --go_out=/out --go_opt=paths=source_relative internal/storage/fs/recordpb/record.proto
RUN cmp /out/internal/storage/fs/recordpb/record.pb.go internal/storage/fs/recordpb/record.pb.go

FROM native AS minio
WORKDIR /minio
ADD https://codeload.github.com/minio/minio/tar.gz/refs/tags/RELEASE.2025-10-15T17-29-55Z /tmp/minio.tar.gz
RUN echo 'be6d0bd3696c3a13a35f02d3a0280b64319c67918b4501c5c3d87f96d000085c  /tmp/minio.tar.gz' | sha256sum -c - && tar -xzf /tmp/minio.tar.gz --strip-components=1 && go build -trimpath -ldflags '-s -w' -o /out/minio .

FROM native AS rustfs
ADD https://github.com/rustfs/rustfs/releases/download/1.0.1/rustfs-linux-x86_64-musl-v1.0.1.deb /tmp/rustfs.deb
RUN echo 'fdb9d5fe1693072029e7bcaa5f2b89a668f28b881f2aa07b5166d23772205dd9  /tmp/rustfs.deb' | sha256sum -c - && dpkg-deb -x /tmp/rustfs.deb /tmp/rustfs-package && find /tmp/rustfs-package -type f -name rustfs -perm /111 -exec cp {} /out/rustfs \; && /out/rustfs --version

FROM eclipse-temurin:17.0.16_8-jre-jammy@sha256:e90fd2b084488c0ffdd22610d44c1579e63b585c7fb83489fd815c0dc5b4e1f7 AS java
FROM node:25.9.0-bookworm-slim@sha256:81db02c4b671288a03915da9534dbd54f96d0e7c24d80ccc54f5b36b2e684370
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates procps libstdc++6 && rm -rf /var/lib/apt/lists/*
ENV JAVA_HOME=/opt/java/openjdk PATH=/opt/java/openjdk/bin:$PATH
COPY --from=java /opt/java/openjdk /opt/java/openjdk
COPY --from=native /out/stow-s3 /app/bin/stow-s3
COPY --from=native /out/embedded.test /app/bin/embedded.test
COPY --from=native /out/benchmark-go.sum /app/generated/benchmark-go.sum
COPY --from=profiling /out/pprof /app/bin/pprof
COPY --from=profiling /out/fs.test /app/bin/fs.test
COPY --from=profiling /out/runtime.test /app/bin/runtime.test
COPY --from=profiling /out/internal/storage/fs/recordpb/record.pb.go /app/generated/record.pb.go
COPY --from=minio /out/minio /app/bin/minio
COPY --from=rustfs /out/rustfs /app/bin/rustfs
ADD https://github.com/gaul/s3proxy/releases/download/s3proxy-4.1.1/s3proxy /app/bin/s3proxy.jar
RUN echo 'cd2e81919e7e1b8d8a23750c9792bb306a2f841a0222a80919b72e989eedd94b  /app/bin/s3proxy.jar' | sha256sum -c -
WORKDIR /app
COPY packages/stow-s3/package.json packages/stow-s3/package-lock.json ./packages/stow-s3/
RUN npm ci --ignore-scripts --omit=optional --prefix packages/stow-s3
COPY packages/stow-s3/dist ./packages/stow-s3/dist
COPY packages/stow-s3/scripts/performance ./packages/stow-s3/scripts/performance
COPY benchmark ./benchmark
RUN node --test benchmark/test/controls.test.mjs
RUN mkdir /tmp/node-agent-checks && node --input-type=module -e 'import {runNodeAgents} from "./benchmark/node-agents.mjs"; console.log(JSON.stringify(await runNodeAgents({samples:2},"/tmp/node-agent-checks")));'
RUN node benchmark/identity.mjs
CMD ["node", "benchmark/target.mjs"]
