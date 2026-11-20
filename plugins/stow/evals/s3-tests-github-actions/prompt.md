---
tags: [positive]
runs: 3
max_turns: 6
timeout_seconds: 240
allowed_tools: [Read, Glob, Grep, Skill]
---

Our Node integration tests need S3 and we want them to run in GitHub Actions without a service container. What is the least setup?
