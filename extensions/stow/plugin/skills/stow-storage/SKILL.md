---
name: stow-storage
description: Browse and reference objects in a configured Stow bucket using its local read-only MCP server.
---

Use `stow_browser` to open the bucket browser or list a page by key prefix.
Continue with its opaque `next_cursor` when `truncated` is true. Pages contain at
most 50 objects. Read a returned `stow://objects/...` resource URI to retrieve
bounded current bytes. Preserve returned URIs exactly; do not construct host paths
or select another bucket. Missing or oversized resources cannot be previewed.

In a compatible MCP App host, selecting a checkbox attaches the object's resource
reference to composer context. It does not submit a message or save the object.
Composer mention search matches key prefixes and may be available on desktop
hosts that implement OpenAI MCP Extensions. Ordinary MCP clients can use tools
and resources without the UI.

This packaged profile is read-only. Never claim that a read or reference creates a
checkpoint, a historical snapshot, or a durable save. Resource references resolve
current bytes, which may change later. The existing guarded-save workflow requires
separate explicit write authorization and a deliberately configured writable host.
