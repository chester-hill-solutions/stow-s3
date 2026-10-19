# Workspace uncertainty and create-only overwrite — 2026-10-19

This closes C04 from the
[bounded closure register](2026-10-18-correctness-closure.md). A workspace path
that could not be read was reported as an absent object, and a create-only write
over that answer replaced bytes that were still there.

## The defect

`internal/storage/workspace` converted every failure to open or stat a path into
`storage.ErrObjectNotFound`. The conversion was in three places: the rooted open
in `rooted_reads.go`, the path resolution in `resolve.go`, and the read in
`objects.go`. A file whose mode denies reading produced "not found", the
`putLocked` precondition check saw no existing object, and a create-only write
was admitted over it.

The workspace contract already states the rule this broke:
"refusing is not the same as claiming the object is absent". The
`internal/rooted` package distinguished only one refusal, `ErrUnsupported`, and
every other failure was treated as absence. The sibling filesystem backend
already used the correct shape, converting only `os.IsNotExist` and propagating
the rest.

The reproduction uses a real permission change on a real workspace file. It uses
no custom store, no mocked absence error and no production hook.

## The correction

Only a confirmed absence is reported as absence. A path that could not be
examined returns `storage.ErrObjectUncertain` with its cause joined, so a caller
can tell a permission refusal from a device fault and can tell uncertainty from
absence. `internal/rooted` now reports `ErrSymlink` and `ErrNotRegular`, so a
path that names a link or another non-regular file keeps its existing answer
while a path that merely could not be read does not.

The read path, the path resolution and the listing surface all carry the
distinction. The listing previously omitted a file it could not open, which is
the same defect by another route: an object missing from a listing is an object
a caller concludes was deleted.

Create-only writes are refused over an uncertain destination rather than
admitted, and the original bytes survive the refusal and a later permission
repair.

## What did not change

True absence is still absence, and a create-only write to a genuinely absent
key still creates the object. A readable object still refuses a create-only
write with `ErrPreconditionFailed`. A symlinked key is still not an object. No
public API, dependency, storage format or index changes.

One residue is recorded rather than fixed. `openRead` still reports a file
replaced between the resolution and the open as absent. That is a confirmed
identity change rather than an I/O uncertainty, and correcting it is a separate
change.

## Validation

All execution is on the Railway scratch runner, in the scratch deployment
`6bfc5a11-81ca-41f3-82c9-202952e34b70` of the `stow-conformance-scratch`
project, with the test steps running as UID 10001. A root process can read a
file with no permissions, so the reproduction would pass without proving
anything if the steps ran as root.

The red steps run the pre-fix revisions of the changed files through a Go
overlay, so the failure they report is the product's answer and not a build
error. The green steps run the corrected code.

The runner needs three things this repository's gates assume, and a scratch
container does not provide by default. The steps must run as an unprivileged
user. The tree needs a Git repository, because the quality and documentation
gates read a baseline from it, and the repository holds the parent's baseline
so the quality gate compares the current tree against the revision it
replaces. And `*.localhost` must resolve to loopback, because the run-through
addressing tests address a stub through that name and a request that never
leaves the process is indistinguishable from one addressed wrongly. The
runner, its image and the plan are saved with the evidence.

One package is left out of the suite. `internal/parentwatch` tests the
parent-death signal, and this container does not deliver it: a standard-library
probe in the evidence shows a child surviving its parent's SIGKILL. The
package depends on nothing outside the standard library, so it cannot be
affected by this change, and its test passes in the GitHub Actions lanes. The
package is run as a separate step that expects the failure, and the probe is
run beside it, so the limitation is recorded rather than assumed.

[Evidence](../assessment-evidence/2026-10-19-workspace-uncertainty/).

## Limitations

This is a bounded correction of one reproduced defect. It does not certify
physical crash behaviour, every platform, or live-provider compatibility, and
the three remaining required fixes in the closure register are unaffected. The
parent-death signal is not qualified on this runner, for the reason recorded
above.
