---
tags: [positive]
runs: 3
max_turns: 6
timeout_seconds: 240
allowed_tools: [Read, Glob, Grep, Skill]
---

An agent does a long coding task in a disposable container. If the container dies we lose everything. I want the files to survive and be resumable on the next machine.
