# Benchmark infrastructure

This partial owns only the two benchmark services and their volumes.
Use the existing project's `benchmark` environment.
Create the shared `BENCH_CONTROL_TOKEN` as a random Railway secret before planning.
The source uses references and contains no secret values or platform IDs.

Run `npm ci` from `benchmark/` before planning.
Link this folder to the intended project and environment.
Save the plan under the ignored `benchmark/plans/` directory.
Review the saved plan before applying it.
See [the benchmark guide](../README.md) for deploy and run commands.
