FROM node:25.9.0-bookworm-slim@sha256:81db02c4b671288a03915da9534dbd54f96d0e7c24d80ccc54f5b36b2e684370 AS verify
WORKDIR /src
ENV BENCH_CLOUD_VERIFY=1 PLAYWRIGHT_BROWSERS_PATH=/tmp/benchmark-playwright
RUN apt-get update && apt-get install -y --no-install-recommends git && rm -rf /var/lib/apt/lists/*
COPY . .
RUN npm ci --ignore-scripts --prefix packages/stow-s3 && node packages/stow-s3/node_modules/playwright/cli.js install --with-deps chromium
RUN bash -o pipefail -c "node benchmark/verify-product-site.mjs 2>&1 | tee /out-site.log"
RUN cp /out-site.log /out/verification/site.log
FROM node:25.9.0-bookworm-slim@sha256:81db02c4b671288a03915da9534dbd54f96d0e7c24d80ccc54f5b36b2e684370
WORKDIR /app
COPY --from=verify /out/verification /app/verification
COPY benchmark ./benchmark
CMD ["node", "benchmark/runner.mjs"]
