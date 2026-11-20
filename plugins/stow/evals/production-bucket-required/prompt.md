---
tags: [negative]
runs: 3
max_turns: 6
timeout_seconds: 240
allowed_tools: [Read, Glob, Grep, Skill]
---

Our compliance rules require all uploads go to our production S3 bucket in eu-west-1 with our IAM role, and policy forbids local emulators or additional storage tools. Show the exact IAM policy and bucket wiring.
