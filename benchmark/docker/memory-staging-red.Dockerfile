FROM golang:1.25.6-bookworm@sha256:f4490d7b261d73af4543c46ac6597d7d101b6e1755bcdd8c5159fda7046b6b3e AS red
ENV GOTOOLCHAIN=local
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
COPY benchmark/source.json ./source.json
RUN mkdir /out && go test ./internal/runtime -run '^(TestMemoryStreamPreparationDoesNotBlockReads|TestBufferedPutStagesAndRejectsReset)$' -count=1 -v > /out/red.log 2>&1; result=$?; cat /out/red.log; test "$result" -eq 1 && grep -q 'memory body preparation holds the runtime lock' /out/red.log && grep -q 'buffered PUT bypassed staging' /out/red.log
RUN cp source.json /out/source.json
FROM node:25.9.0-bookworm-slim@sha256:81db02c4b671288a03915da9534dbd54f96d0e7c24d80ccc54f5b36b2e684370
WORKDIR /app
COPY --from=red /out ./verification
COPY benchmark ./benchmark
CMD ["node", "benchmark/runner.mjs"]
