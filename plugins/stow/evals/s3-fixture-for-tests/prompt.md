---
tags: [positive]
runs: 3
max_turns: 6
timeout_seconds: 120
allowed_tools: [Read, Glob, Grep, Skill]
---

Our Go test suite needs an S3 bucket to upload fixtures to. We don't want to hit real AWS in CI and the runners have no Docker. Set up something the tests can point at, and tell me exactly what to change.
