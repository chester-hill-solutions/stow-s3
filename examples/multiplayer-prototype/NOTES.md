# Multiplayer experiment — 2026-09-29

Question: can shared files, selective explanations and checkpoint recovery provide
a useful collaboration model without making Git worktrees the unit of coordination?

## Observed

Three real OpenCode participants used the existing local
`opencode/space-bunny-free` model in one Stow directory. Their admission timestamps
overlapped. The designer was deliberately interrupted immediately after admission;
the analyst and auditor continued concurrently for about 19 seconds. After both
settled, a fresh designer session read its subscribed report and built the page.
The full task flow took about 63 seconds.

The analyst changed the provisional contract to integer cents and explicit counts.
The designer's embedded data and rendered page matched an independent calculation:
**$520.00, 6 orders, 17 units**, with North/West $190 each, South $140 and two $260
channels. The browser displayed those values. The auditor wrote its source-quality
report in one turn and received no peer report or follow-up turn.

Three publications produced **one delivery**, versus six possible deliveries if
every publication had been broadcast to both peers. This measures routing only;
it is not a measured token or cost saving. The filesystem listener made zero model
polls. Four model turns include the deliberately interrupted designer turn.

The normal real-agent run also passed, including reusing the designer's existing
session for its second turn. It produced four reports with one delivery, versus
eight possible broadcast deliveries, and restored checkpoint
`cp_661d9fae24c222f6b5994ccb`. Its scratch evidence is under
`stow-multiplayer-PROTOTYPE-nLm2SG/evidence.json` beside the interrupted run above.

All writers stopped before capture. Checkpoint `cp_c8126642f9a14c8e4e4e9081` restored
the site, metrics, audit, immutable analyst report and portable room state byte for
byte. A new room with fresh external state replayed the report only to its recorded
designer recipient. It restored no ownership claims. Local acknowledgment happened
after capture confirmation.

Scratch evidence from this machine:

```text
/private/var/folders/f3/n5tk3w792px0vctvqjytc6t00000gn/T/stow-multiplayer-PROTOTYPE-ZF5al1/evidence.json
```

`make multiplayer-check` passed both offline scenarios with three overlapping
actors, conflict refusal and verified capture/restore. OpenCode adapter regression
tests passed: 27 passed, one opt-in model test skipped; the actual multiplayer model
run above was performed separately. The documentation command check passed.

The first real run failed: ambiguous `unit_price` units led the analyst to use cents
instead of dollars, and the designer exhausted its three-minute turn budget while
planning without writing. Making the source contract explicit and bounding the
page and notes resolved both in the revised run. Typed report transport cannot
replace clear task/data contracts or independent validation.

## What this suggests

The useful product nouns are **workspace, participant, owned files, subscription,
report and checkpoint**. Directory isolation can remain an implementation choice.
An explanation tied to actual changed files is more useful than forwarding every
agent transcript. Idle subscribers should stay listening even after releasing their
editing claims. Delivery queues work at safe turn boundaries; receiving a report
does not authorize a second simultaneous turn for the same participant.

Stow already supplied preparation, a directory holder, capture and restore. The
experimental caller supplied assignments, model sessions, ownership, event routing
and writer settlement. No new storage API was required for this local demonstration.

## Still unproven

This is one coordinator on one host, with a finite scenario and three trusted
file-only agents. It is not human multiplayer, a general scheduler or a distributed
lease service. The interruption is an early controlled process cancellation, not a
coordinator crash after partial writes. Recovery uses current files before capture,
not migration of an in-flight process. The restored room needs explicit registration.

Next useful experiments: inject failure after an output write but before report
publication; reconcile applied-message receipts against pending replay without an
extra model turn; then run a second coordinator/host with explicit ownership
transfer. Human presence, remote transport/authentication and shared-editor conflict
resolution belong to a product layer if that direction proves worthwhile.

Global writer pause remains the checkpoint consistency barrier. Per-file checkpoints,
atomic multi-file commits, message retention/backpressure, directory rename handling,
malicious participant containment and many-agent workload costs remain unexplored.
Delete or absorb this prototype once those product choices are settled.

## Subsequent product direction

The user subsequently chose shared live editing without file claims: agents know who
is present, what they are doing and their last read/edit locations, and adapt to relevant
peer/dependency changes. See the [product plan](../../docs/realtime-multiplayer-plan.md)
and [engineering detail](../../docs/realtime-multiplayer-engineering.md). This prototype
remains dated evidence of its earlier ownership-based model; it does not implement
the new experience. The latest RT-0 starts with continuous native activity/file
observation and shadow decisions; RT-1 tests useful awareness and bounded steering
before conditional guarded-editor expansion. Native observation does not prevent stale
writes. Current RT work lives in the [standalone collaboration repository](https://github.com/chester-hill-solutions/gangcode/blob/main/docs/plan.md). This example remains historical evidence.
