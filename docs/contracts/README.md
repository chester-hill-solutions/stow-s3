# Interface contracts

These references define supported behavior and explicit limits. Consult the
[usage guides](../guides/README.md) for runnable workflows and the
[API reference](../api/README.md) for concrete method signatures.

| Contract | Scope |
| --- | --- |
| [S3 HTTP API](compat-contract.md) | Routes, headers, conditions and refused operations |
| [Workspace contract](../workspace-contract.md) | Native workspace ownership, lifecycle and guarantees |
| [Workspace case vocabulary](workspace-contract-steps.md) | Shared conformance steps and driver behavior |
| [Task manifests](task-manifest.md) | Inputs, Git checkouts, preparation and limits |
| [Object MCP](object-mcp-contract.md) | Native object operations exposed through MCP |
| [Native workspace file saves](workspace-editing-contract.md) | W26 C.1 local read observations implemented; guarded saves and durable receipts remain proposed |
| [Checkpoint resources](checkpoint-resources-contract.md) | W25 local saved-file views, ephemeral retention and verified bounded ranges |
| [Workspace resources](workspace-resources-contract.md) | W23/W24 read-only file resources, scoped pages, optional browser and attachment lifetime |
| [Identity and admission](../storage-admission-contract.md) | Foundation semantics and the boundary between current profiles and target guarantees |

The primary workspace and admission contracts keep their established repository
paths. Accepted [architecture decisions](../adr/README.md) govern design;
acceptance alone does not establish that a proposed API is implemented.

[All documentation](../README.md)
