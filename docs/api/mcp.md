# MCP API

Connect an MCP client to the native server over stdio. The host selects the object directory and bucket, or the workspace registry and ID.
Tool arguments cannot select another backend or widen that scope.

## Object server

```sh
stow-s3 mcp --object-dir /absolute/objects --bucket files --create-bucket
```

Use `--read-only` to reject saves. Add `--browser` for browsing and object-reference tools.
Close the connection to release ownership. Object data and save receipts remain.

## Workspace server

```sh
stow-s3 mcp --registry-dir /absolute/registry --workspace-id WORKSPACE_ID \
  --timeout 30s --max-bytes 1073741824 --max-files 100000
```

The capture deadline must be positive. Pause writers before capture.
Set `--export-root` to enable handoff export. Also set `--adopt-root` to enable adoption.
Those directories must already exist. Bundle and destination names are direct children of the configured roots.

## Workspace file resources

Add `--workspace-files` to expose current regular files in the configured workspace.
Add `--resources-only` to omit tools and prompts; this requires `--workspace-files`.
Both flags are incompatible with object mode. The workspace timeout must remain positive.
File resources always provide read-only access, including when checkpoint tools are enabled.
Repeat `--read-root inputs --read-root reports` to expose selected existing relative directories.
The default scope is the whole workspace; up to 16 distinct roots are allowed. Mandatory exclusions always apply.
Add `--browser` for the optional workspace App and its read-only launcher tool; it requires
`--workspace-files` and cannot be combined with `--resources-only`.
Readers coexist with the workspace writer and protect registered deletion until close.
All processes removing a workspace with readers attached must support reader leases.
Private paths and symlinks are excluded; resource reads do not create save observations.
Live reads are mutable observations, not checkpoints. See the [workspace resource contract](../contracts/workspace-resources-contract.md).

## Workspace file editing (opt-in)

Add `--workspace-edit --write-root docs --coordination host_quiesced` with
`--workspace-files` to claim the registered workspace owner and expose the four
managed file tools. Repeat `--write-root` to select existing relative directories
within the read scope. The existing owner permissions and storage quotas apply;
capture bounds do not replace them. Quiesce all writers outside this editor.
Object, resource-only, selected-checkpoint and browser modes refuse editing.

Read with `stow_workspace_file_read_for_save` to obtain a live observation token.
Persist a random request key and the intended path/body/options before saving.
Use exactly one `text` or `data_base64` body, and exactly one
`observation_token` or `replace: true`. File bytes are bounded to 64 KiB.
File resources remain reads; they do not create comparison tokens.

After interruption, restart with the same opt-in settings plus
`--recover-file-saves`, then resolve the original path and request key.
Every journal-bearing root requires explicit recovery, including terminal-only
state. Missing receipts and unproven publication remain unknown; never infer
that no write occurred or retry with a new key. Known committed results preserve
the original generation even after later file changes. Checkpoint creation is a
separate caller action after a confirmed save. See the
[workspace editing contract](../contracts/workspace-editing-contract.md).

## Saved checkpoint file resources

Add `--checkpoint-id CHECKPOINT_ID` with `--workspace-files` to select saved
files instead of the mutable view. The manifest must belong to the configured
workspace. Working files may be absent. `--resources-only` still omits tools and
prompts; `--browser` refuses this profile. Static read roots and mandatory exclusions
govern file resources, while normal checkpoint tools retain their existing grants.

The view contains captured filesystem files from v1/v2 manifests. Portable logical
objects are not mapped into fictitious paths. Shared read leases protect the
selected checkpoint and registered workspace removal without blocking capture.
An absolute five-minute lifetime actively closes idle readers; reconnect opens a
new view. Process death releases the ephemeral leases without recovery journal rows.

Whole files remain bounded to 64 KiB. A range captures at most 64 KiB while hashing
the entire saved payload, up to 64 MiB, before returning bytes. Corruption outside
the requested window also refuses the read. Follow file `range_uri` and range
`next_uri` references unchanged. See the [checkpoint resource contract](../contracts/checkpoint-resources-contract.md).

## Limits

| Setting | Value |
| --- | --- |
| Decoded object body | 64 KiB by default; lower with --max-object-bytes |
| Live observations | 256 by default; lower with --max-observations |
| Observation lifetime | 15 minutes |
| Concurrent object calls | 8 |
| Tool arguments / encoded result | 256 KiB each |
| Stdio request line | 1 MiB |
| Workspace inspection page | 100 entries by default; 1–1,000 allowed |
| Bucket browser page | 50 objects |
| Workspace file decoded body / encoded response | 64 KiB / 256 KiB |
| Workspace complete directory resource | 100 visible direct children; 10,000 examined entries; overflow refuses |
| Workspace paged directory observation | 10,000 examined entries / 4 MiB charged metadata; 100 entries and 256 KiB encoded result per page |
| Workspace directory cache | 8 observations / 16 MiB charged metadata; absolute five-minute lifetime |
| Workspace browser HTML / encoded UI response | 2 MiB / 4 MiB; separate from file and directory response limits |
| Saved checkpoint view | 10,000 manifest files / 4 MiB charged metadata; five-minute absolute lifetime |
| Saved whole file / range / full-file verification | 64 KiB / 64 KiB / 64 MiB |
| Concurrent workspace resource reads / deadline | 8 / at most 10 seconds or the configured timeout, whichever is shorter |
| Workspace editing file body / observations / lifetime | 64 KiB / 64 / five minutes |
| Workspace editing arguments / encoded tool result | 256 KiB each; optional observation text is omitted if needed to fit |
| Capture bytes / files | 1 GiB / 100,000 by default; requests can only narrow the host bounds |

These limits do not bound total process memory.

## Save and recovery results

Object results have `version: 1` and `outcome`. Saves use `committed`, `not_committed` or `unknown`.
Reads, capability checks and observation release use `ok`.
`available` says whether the requested object or receipt information is included.
`error` contains a code and message; MCP `isError` is separate from the effect outcome.
A committed outcome remains committed even if response encoding or cleanup reports an error.

1. Persist a unique request_key and the intended key, bytes and options.
2. Read the object to obtain an observation_token, including observed absence.
3. Save with exactly one observation_token or replace: true.
4. After an uncertain reply or server restart, resolve the original key and request_key.

Resolution starts no new save. Missing evidence leaves the outcome unknown.
Observation tokens expire or disappear on restart; receipts remain until explicitly managed.
Receipts retain metadata, not historical bodies. Later reads return current bytes.

Checkpoint results also have `version: 1` and an effect outcome.
Only `committed` confirms capture. Reuse the original request key and options for resolution.
An error includes code, phase, attempts, retryable and message.
Inspection pages return entries and next_cursor; pass that cursor unchanged to continue.

## Resources

- `stow://guides/checkpoints/v1`: checkpoint workflow, text/plain.
- `ui://stow/browser-v1`: optional self-contained bucket browser UI.
- `stow://objects/{bucket}/{key}`: current bounded object contents; key is a base64url identifier returned by the browser tools.
- `stow://workspaces/{id}/v1`: opt-in workspace file overview and navigation roots.
- `stow://workspaces/{id}/tree/{path}`: bounded complete direct-child observation; path is canonical base64url of a relative directory; encode `.` for the root.
- `stow://workspaces/{id}/tree-pages/v1/{path}`: first page of a bounded metadata observation; follow the returned `page.next_uri` unchanged.
- `stow://workspaces/{id}/tree-pages/v1/{path}/{cursor}`: continuation of that observation; expires after five minutes or cache eviction.
- `stow://workspaces/{id}/files/{path}`: bounded current regular-file contents; path is canonical base64url of a relative file.
- `ui://stow/workspace-browser-v1`: optional workspace browser UI, bootstrapped to the configured overview.
- `stow://workspaces/{id}/checkpoints/{checkpoint}/v1`: selected saved-file overview and root URI.
- `stow://workspaces/{id}/checkpoints/{checkpoint}/tree/v1/{path}/{offset}`: saved directory page; omit the offset for its first page.
- `stow://workspaces/{id}/checkpoints/{checkpoint}/files/{path}`: bounded complete saved-file contents.
- `stow://workspaces/{id}/checkpoints/{checkpoint}/ranges/v1/{path}/{offset}/{length}`: verified saved-file window, returned as versioned JSON text or base64 with complete file digest and size.

The workspace overview supplies `paged_root_uri`, selected read roots, capabilities and bounds.
Pages retain directory metadata and ordering, while file reads return current bytes.
They do not establish a snapshot of file contents. Refresh after a cursor expires.

Object references resolve current bytes and do not pin a historical revision.

## Tools

## stow_browser

Object storage. Browse a bounded page in the configured bucket by key prefix. This browser never saves or deletes objects.

### Input

```json
{
  "type": "object",
  "properties": {
    "prefix": {
      "type": "string"
    },
    "cursor": {
      "type": "string"
    }
  },
  "additionalProperties": false
}
```

### Result

```json
{
  "type": "object",
  "properties": {
    "bucket": {
      "type": "string"
    },
    "items": {
      "type": [
        "null",
        "array"
      ],
      "items": {
        "type": "object",
        "properties": {
          "type": {
            "type": "string"
          },
          "uri": {
            "type": "string"
          },
          "name": {
            "type": "string"
          },
          "mimeType": {
            "type": "string"
          },
          "size": {
            "type": "integer"
          }
        },
        "required": [
          "type",
          "uri",
          "name",
          "mimeType",
          "size"
        ],
        "additionalProperties": false
      }
    },
    "truncated": {
      "type": "boolean"
    },
    "next_cursor": {
      "type": "string"
    },
    "max_bytes": {
      "type": "integer"
    }
  },
  "required": [
    "bucket",
    "items",
    "truncated",
    "max_bytes"
  ],
  "additionalProperties": false
}
```
## stow_checkpoint_create

Workspaces. Capture quiescent working data. Persist a random request key before calling; reuse it to reconcile a lost reply.

### Input

```json
{
  "type": "object",
  "properties": {
    "workspace_id": {
      "type": "string"
    },
    "request_key": {
      "type": "string"
    },
    "parent_id": {
      "type": "string"
    },
    "max_bytes": {
      "type": "integer"
    },
    "max_files": {
      "type": "integer"
    }
  },
  "required": [
    "workspace_id",
    "request_key"
  ],
  "additionalProperties": false
}
```

### Result

```json
{
  "type": "object",
  "properties": {
    "version": {
      "type": "integer"
    },
    "outcome": {
      "type": "string"
    },
    "workspace": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "workspace_id": {
          "type": "string"
        },
        "directory": {
          "type": "string"
        },
        "bucket": {
          "type": "string"
        },
        "max_checkpoint_bytes": {
          "type": "integer"
        },
        "max_checkpoints": {
          "type": "integer"
        }
      },
      "required": [
        "workspace_id",
        "directory",
        "bucket",
        "max_checkpoint_bytes",
        "max_checkpoints"
      ],
      "additionalProperties": false
    },
    "capture": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "checkpoint": {
          "type": "object",
          "properties": {
            "version": {
              "type": "integer"
            },
            "objects": {
              "type": "integer"
            },
            "id": {
              "type": "string"
            },
            "workspace_id": {
              "type": "string"
            },
            "parent_id": {
              "type": "string"
            },
            "created": {
              "type": "string"
            },
            "files": {
              "type": "integer"
            },
            "bytes": {
              "type": "integer"
            },
            "excluded": {
              "type": [
                "null",
                "array"
              ],
              "items": {
                "type": "string"
              }
            }
          },
          "required": [
            "id",
            "workspace_id",
            "parent_id",
            "created",
            "files",
            "bytes",
            "excluded"
          ],
          "additionalProperties": false
        },
        "replayed": {
          "type": "boolean"
        },
        "outcome": {
          "type": "string"
        }
      },
      "required": [
        "checkpoint",
        "replayed",
        "outcome"
      ],
      "additionalProperties": false
    },
    "checkpoint_id": {
      "type": "string"
    },
    "entries": {
      "type": [
        "null",
        "array"
      ],
      "items": {
        "type": "object",
        "properties": {
          "object": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "bucket": {
                "type": "string"
              },
              "key": {
                "type": "string"
              },
              "payload": {
                "type": "string"
              },
              "size": {
                "type": "integer"
              },
              "sha256": {
                "type": "string"
              },
              "content_type": {
                "type": "string"
              },
              "metadata": {
                "type": "object",
                "additionalProperties": {
                  "type": "string"
                }
              },
              "checksum_algorithm": {
                "type": "string"
              },
              "checksum_value": {
                "type": "string"
              },
              "checksum_type": {
                "type": "string"
              }
            },
            "required": [
              "bucket",
              "key",
              "payload",
              "size",
              "sha256",
              "content_type"
            ],
            "additionalProperties": false
          },
          "bucket": {
            "type": "string"
          },
          "object_change": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "bucket": {
                "type": "string"
              },
              "key": {
                "type": "string"
              },
              "kind": {
                "type": "string"
              },
              "from": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "bucket": {
                    "type": "string"
                  },
                  "key": {
                    "type": "string"
                  },
                  "payload": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "sha256": {
                    "type": "string"
                  },
                  "content_type": {
                    "type": "string"
                  },
                  "metadata": {
                    "type": "object",
                    "additionalProperties": {
                      "type": "string"
                    }
                  },
                  "checksum_algorithm": {
                    "type": "string"
                  },
                  "checksum_value": {
                    "type": "string"
                  },
                  "checksum_type": {
                    "type": "string"
                  }
                },
                "required": [
                  "bucket",
                  "key",
                  "payload",
                  "size",
                  "sha256",
                  "content_type"
                ],
                "additionalProperties": false
              },
              "to": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "bucket": {
                    "type": "string"
                  },
                  "key": {
                    "type": "string"
                  },
                  "payload": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "sha256": {
                    "type": "string"
                  },
                  "content_type": {
                    "type": "string"
                  },
                  "metadata": {
                    "type": "object",
                    "additionalProperties": {
                      "type": "string"
                    }
                  },
                  "checksum_algorithm": {
                    "type": "string"
                  },
                  "checksum_value": {
                    "type": "string"
                  },
                  "checksum_type": {
                    "type": "string"
                  }
                },
                "required": [
                  "bucket",
                  "key",
                  "payload",
                  "size",
                  "sha256",
                  "content_type"
                ],
                "additionalProperties": false
              }
            },
            "required": [
              "bucket",
              "key",
              "kind"
            ],
            "additionalProperties": false
          },
          "bucket_change": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "bucket": {
                "type": "string"
              },
              "kind": {
                "type": "string"
              }
            },
            "required": [
              "bucket",
              "kind"
            ],
            "additionalProperties": false
          },
          "kind": {
            "type": "string"
          },
          "file": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "path": {
                "type": "string"
              },
              "size": {
                "type": "integer"
              },
              "mode": {
                "type": "integer",
                "minimum": 0,
                "maximum": 4294967295
              },
              "sha256": {
                "type": "string"
              }
            },
            "required": [
              "path",
              "size",
              "mode",
              "sha256"
            ],
            "additionalProperties": false
          },
          "change": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "path": {
                "type": "string"
              },
              "kind": {
                "type": "string"
              },
              "from": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "path": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "mode": {
                    "type": "integer",
                    "minimum": 0,
                    "maximum": 4294967295
                  },
                  "sha256": {
                    "type": "string"
                  }
                },
                "required": [
                  "path",
                  "size",
                  "mode",
                  "sha256"
                ],
                "additionalProperties": false
              },
              "to": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "path": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "mode": {
                    "type": "integer",
                    "minimum": 0,
                    "maximum": 4294967295
                  },
                  "sha256": {
                    "type": "string"
                  }
                },
                "required": [
                  "path",
                  "size",
                  "mode",
                  "sha256"
                ],
                "additionalProperties": false
              }
            },
            "required": [
              "path",
              "kind"
            ],
            "additionalProperties": false
          },
          "excluded": {
            "type": "string"
          }
        },
        "required": [
          "kind"
        ],
        "additionalProperties": false
      }
    },
    "next_cursor": {
      "type": "string"
    },
    "handoff": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "version": {
          "type": "integer"
        },
        "workspace_id": {
          "type": "string"
        },
        "checkpoint_id": {
          "type": "string"
        },
        "registry_dir": {
          "type": "string"
        },
        "team": {
          "type": "string"
        },
        "archive": {
          "type": [
            "null",
            "object"
          ],
          "properties": {
            "path": {
              "type": "string"
            },
            "sha256": {
              "type": "string"
            },
            "bytes": {
              "type": "integer"
            },
            "files": {
              "type": "integer"
            }
          },
          "required": [
            "path",
            "sha256",
            "bytes",
            "files"
          ],
          "additionalProperties": false
        }
      },
      "required": [
        "version",
        "workspace_id"
      ],
      "additionalProperties": false
    },
    "bundle": {
      "type": "string"
    },
    "origin_checkpoint_id": {
      "type": "string"
    },
    "error": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "code": {
          "type": "string"
        },
        "phase": {
          "type": "string"
        },
        "attempts": {
          "type": "integer"
        },
        "retryable": {
          "type": "boolean"
        },
        "message": {
          "type": "string"
        }
      },
      "required": [
        "code",
        "phase",
        "attempts",
        "retryable",
        "message"
      ],
      "additionalProperties": false
    }
  },
  "required": [
    "version",
    "outcome"
  ],
  "additionalProperties": false
}
```
## stow_checkpoint_diff

Workspaces. Compare two checkpoints in the configured workspace.

### Input

```json
{
  "type": "object",
  "properties": {
    "from_id": {
      "type": "string"
    },
    "to_id": {
      "type": "string"
    },
    "limit": {
      "type": "integer"
    },
    "cursor": {
      "type": "string"
    }
  },
  "required": [
    "from_id",
    "to_id"
  ],
  "additionalProperties": false
}
```

### Result

```json
{
  "type": "object",
  "properties": {
    "version": {
      "type": "integer"
    },
    "outcome": {
      "type": "string"
    },
    "workspace": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "workspace_id": {
          "type": "string"
        },
        "directory": {
          "type": "string"
        },
        "bucket": {
          "type": "string"
        },
        "max_checkpoint_bytes": {
          "type": "integer"
        },
        "max_checkpoints": {
          "type": "integer"
        }
      },
      "required": [
        "workspace_id",
        "directory",
        "bucket",
        "max_checkpoint_bytes",
        "max_checkpoints"
      ],
      "additionalProperties": false
    },
    "capture": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "checkpoint": {
          "type": "object",
          "properties": {
            "version": {
              "type": "integer"
            },
            "objects": {
              "type": "integer"
            },
            "id": {
              "type": "string"
            },
            "workspace_id": {
              "type": "string"
            },
            "parent_id": {
              "type": "string"
            },
            "created": {
              "type": "string"
            },
            "files": {
              "type": "integer"
            },
            "bytes": {
              "type": "integer"
            },
            "excluded": {
              "type": [
                "null",
                "array"
              ],
              "items": {
                "type": "string"
              }
            }
          },
          "required": [
            "id",
            "workspace_id",
            "parent_id",
            "created",
            "files",
            "bytes",
            "excluded"
          ],
          "additionalProperties": false
        },
        "replayed": {
          "type": "boolean"
        },
        "outcome": {
          "type": "string"
        }
      },
      "required": [
        "checkpoint",
        "replayed",
        "outcome"
      ],
      "additionalProperties": false
    },
    "checkpoint_id": {
      "type": "string"
    },
    "entries": {
      "type": [
        "null",
        "array"
      ],
      "items": {
        "type": "object",
        "properties": {
          "object": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "bucket": {
                "type": "string"
              },
              "key": {
                "type": "string"
              },
              "payload": {
                "type": "string"
              },
              "size": {
                "type": "integer"
              },
              "sha256": {
                "type": "string"
              },
              "content_type": {
                "type": "string"
              },
              "metadata": {
                "type": "object",
                "additionalProperties": {
                  "type": "string"
                }
              },
              "checksum_algorithm": {
                "type": "string"
              },
              "checksum_value": {
                "type": "string"
              },
              "checksum_type": {
                "type": "string"
              }
            },
            "required": [
              "bucket",
              "key",
              "payload",
              "size",
              "sha256",
              "content_type"
            ],
            "additionalProperties": false
          },
          "bucket": {
            "type": "string"
          },
          "object_change": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "bucket": {
                "type": "string"
              },
              "key": {
                "type": "string"
              },
              "kind": {
                "type": "string"
              },
              "from": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "bucket": {
                    "type": "string"
                  },
                  "key": {
                    "type": "string"
                  },
                  "payload": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "sha256": {
                    "type": "string"
                  },
                  "content_type": {
                    "type": "string"
                  },
                  "metadata": {
                    "type": "object",
                    "additionalProperties": {
                      "type": "string"
                    }
                  },
                  "checksum_algorithm": {
                    "type": "string"
                  },
                  "checksum_value": {
                    "type": "string"
                  },
                  "checksum_type": {
                    "type": "string"
                  }
                },
                "required": [
                  "bucket",
                  "key",
                  "payload",
                  "size",
                  "sha256",
                  "content_type"
                ],
                "additionalProperties": false
              },
              "to": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "bucket": {
                    "type": "string"
                  },
                  "key": {
                    "type": "string"
                  },
                  "payload": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "sha256": {
                    "type": "string"
                  },
                  "content_type": {
                    "type": "string"
                  },
                  "metadata": {
                    "type": "object",
                    "additionalProperties": {
                      "type": "string"
                    }
                  },
                  "checksum_algorithm": {
                    "type": "string"
                  },
                  "checksum_value": {
                    "type": "string"
                  },
                  "checksum_type": {
                    "type": "string"
                  }
                },
                "required": [
                  "bucket",
                  "key",
                  "payload",
                  "size",
                  "sha256",
                  "content_type"
                ],
                "additionalProperties": false
              }
            },
            "required": [
              "bucket",
              "key",
              "kind"
            ],
            "additionalProperties": false
          },
          "bucket_change": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "bucket": {
                "type": "string"
              },
              "kind": {
                "type": "string"
              }
            },
            "required": [
              "bucket",
              "kind"
            ],
            "additionalProperties": false
          },
          "kind": {
            "type": "string"
          },
          "file": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "path": {
                "type": "string"
              },
              "size": {
                "type": "integer"
              },
              "mode": {
                "type": "integer",
                "minimum": 0,
                "maximum": 4294967295
              },
              "sha256": {
                "type": "string"
              }
            },
            "required": [
              "path",
              "size",
              "mode",
              "sha256"
            ],
            "additionalProperties": false
          },
          "change": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "path": {
                "type": "string"
              },
              "kind": {
                "type": "string"
              },
              "from": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "path": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "mode": {
                    "type": "integer",
                    "minimum": 0,
                    "maximum": 4294967295
                  },
                  "sha256": {
                    "type": "string"
                  }
                },
                "required": [
                  "path",
                  "size",
                  "mode",
                  "sha256"
                ],
                "additionalProperties": false
              },
              "to": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "path": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "mode": {
                    "type": "integer",
                    "minimum": 0,
                    "maximum": 4294967295
                  },
                  "sha256": {
                    "type": "string"
                  }
                },
                "required": [
                  "path",
                  "size",
                  "mode",
                  "sha256"
                ],
                "additionalProperties": false
              }
            },
            "required": [
              "path",
              "kind"
            ],
            "additionalProperties": false
          },
          "excluded": {
            "type": "string"
          }
        },
        "required": [
          "kind"
        ],
        "additionalProperties": false
      }
    },
    "next_cursor": {
      "type": "string"
    },
    "handoff": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "version": {
          "type": "integer"
        },
        "workspace_id": {
          "type": "string"
        },
        "checkpoint_id": {
          "type": "string"
        },
        "registry_dir": {
          "type": "string"
        },
        "team": {
          "type": "string"
        },
        "archive": {
          "type": [
            "null",
            "object"
          ],
          "properties": {
            "path": {
              "type": "string"
            },
            "sha256": {
              "type": "string"
            },
            "bytes": {
              "type": "integer"
            },
            "files": {
              "type": "integer"
            }
          },
          "required": [
            "path",
            "sha256",
            "bytes",
            "files"
          ],
          "additionalProperties": false
        }
      },
      "required": [
        "version",
        "workspace_id"
      ],
      "additionalProperties": false
    },
    "bundle": {
      "type": "string"
    },
    "origin_checkpoint_id": {
      "type": "string"
    },
    "error": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "code": {
          "type": "string"
        },
        "phase": {
          "type": "string"
        },
        "attempts": {
          "type": "integer"
        },
        "retryable": {
          "type": "boolean"
        },
        "message": {
          "type": "string"
        }
      },
      "required": [
        "code",
        "phase",
        "attempts",
        "retryable",
        "message"
      ],
      "additionalProperties": false
    }
  },
  "required": [
    "version",
    "outcome"
  ],
  "additionalProperties": false
}
```
## stow_checkpoint_inspect

Workspaces. Inspect bounded pages of saved checkpoint entries and exclusions.

### Input

```json
{
  "type": "object",
  "properties": {
    "checkpoint_id": {
      "type": "string"
    },
    "limit": {
      "type": "integer"
    },
    "cursor": {
      "type": "string"
    }
  },
  "required": [
    "checkpoint_id"
  ],
  "additionalProperties": false
}
```

### Result

```json
{
  "type": "object",
  "properties": {
    "version": {
      "type": "integer"
    },
    "outcome": {
      "type": "string"
    },
    "workspace": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "workspace_id": {
          "type": "string"
        },
        "directory": {
          "type": "string"
        },
        "bucket": {
          "type": "string"
        },
        "max_checkpoint_bytes": {
          "type": "integer"
        },
        "max_checkpoints": {
          "type": "integer"
        }
      },
      "required": [
        "workspace_id",
        "directory",
        "bucket",
        "max_checkpoint_bytes",
        "max_checkpoints"
      ],
      "additionalProperties": false
    },
    "capture": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "checkpoint": {
          "type": "object",
          "properties": {
            "version": {
              "type": "integer"
            },
            "objects": {
              "type": "integer"
            },
            "id": {
              "type": "string"
            },
            "workspace_id": {
              "type": "string"
            },
            "parent_id": {
              "type": "string"
            },
            "created": {
              "type": "string"
            },
            "files": {
              "type": "integer"
            },
            "bytes": {
              "type": "integer"
            },
            "excluded": {
              "type": [
                "null",
                "array"
              ],
              "items": {
                "type": "string"
              }
            }
          },
          "required": [
            "id",
            "workspace_id",
            "parent_id",
            "created",
            "files",
            "bytes",
            "excluded"
          ],
          "additionalProperties": false
        },
        "replayed": {
          "type": "boolean"
        },
        "outcome": {
          "type": "string"
        }
      },
      "required": [
        "checkpoint",
        "replayed",
        "outcome"
      ],
      "additionalProperties": false
    },
    "checkpoint_id": {
      "type": "string"
    },
    "entries": {
      "type": [
        "null",
        "array"
      ],
      "items": {
        "type": "object",
        "properties": {
          "object": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "bucket": {
                "type": "string"
              },
              "key": {
                "type": "string"
              },
              "payload": {
                "type": "string"
              },
              "size": {
                "type": "integer"
              },
              "sha256": {
                "type": "string"
              },
              "content_type": {
                "type": "string"
              },
              "metadata": {
                "type": "object",
                "additionalProperties": {
                  "type": "string"
                }
              },
              "checksum_algorithm": {
                "type": "string"
              },
              "checksum_value": {
                "type": "string"
              },
              "checksum_type": {
                "type": "string"
              }
            },
            "required": [
              "bucket",
              "key",
              "payload",
              "size",
              "sha256",
              "content_type"
            ],
            "additionalProperties": false
          },
          "bucket": {
            "type": "string"
          },
          "object_change": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "bucket": {
                "type": "string"
              },
              "key": {
                "type": "string"
              },
              "kind": {
                "type": "string"
              },
              "from": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "bucket": {
                    "type": "string"
                  },
                  "key": {
                    "type": "string"
                  },
                  "payload": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "sha256": {
                    "type": "string"
                  },
                  "content_type": {
                    "type": "string"
                  },
                  "metadata": {
                    "type": "object",
                    "additionalProperties": {
                      "type": "string"
                    }
                  },
                  "checksum_algorithm": {
                    "type": "string"
                  },
                  "checksum_value": {
                    "type": "string"
                  },
                  "checksum_type": {
                    "type": "string"
                  }
                },
                "required": [
                  "bucket",
                  "key",
                  "payload",
                  "size",
                  "sha256",
                  "content_type"
                ],
                "additionalProperties": false
              },
              "to": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "bucket": {
                    "type": "string"
                  },
                  "key": {
                    "type": "string"
                  },
                  "payload": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "sha256": {
                    "type": "string"
                  },
                  "content_type": {
                    "type": "string"
                  },
                  "metadata": {
                    "type": "object",
                    "additionalProperties": {
                      "type": "string"
                    }
                  },
                  "checksum_algorithm": {
                    "type": "string"
                  },
                  "checksum_value": {
                    "type": "string"
                  },
                  "checksum_type": {
                    "type": "string"
                  }
                },
                "required": [
                  "bucket",
                  "key",
                  "payload",
                  "size",
                  "sha256",
                  "content_type"
                ],
                "additionalProperties": false
              }
            },
            "required": [
              "bucket",
              "key",
              "kind"
            ],
            "additionalProperties": false
          },
          "bucket_change": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "bucket": {
                "type": "string"
              },
              "kind": {
                "type": "string"
              }
            },
            "required": [
              "bucket",
              "kind"
            ],
            "additionalProperties": false
          },
          "kind": {
            "type": "string"
          },
          "file": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "path": {
                "type": "string"
              },
              "size": {
                "type": "integer"
              },
              "mode": {
                "type": "integer",
                "minimum": 0,
                "maximum": 4294967295
              },
              "sha256": {
                "type": "string"
              }
            },
            "required": [
              "path",
              "size",
              "mode",
              "sha256"
            ],
            "additionalProperties": false
          },
          "change": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "path": {
                "type": "string"
              },
              "kind": {
                "type": "string"
              },
              "from": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "path": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "mode": {
                    "type": "integer",
                    "minimum": 0,
                    "maximum": 4294967295
                  },
                  "sha256": {
                    "type": "string"
                  }
                },
                "required": [
                  "path",
                  "size",
                  "mode",
                  "sha256"
                ],
                "additionalProperties": false
              },
              "to": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "path": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "mode": {
                    "type": "integer",
                    "minimum": 0,
                    "maximum": 4294967295
                  },
                  "sha256": {
                    "type": "string"
                  }
                },
                "required": [
                  "path",
                  "size",
                  "mode",
                  "sha256"
                ],
                "additionalProperties": false
              }
            },
            "required": [
              "path",
              "kind"
            ],
            "additionalProperties": false
          },
          "excluded": {
            "type": "string"
          }
        },
        "required": [
          "kind"
        ],
        "additionalProperties": false
      }
    },
    "next_cursor": {
      "type": "string"
    },
    "handoff": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "version": {
          "type": "integer"
        },
        "workspace_id": {
          "type": "string"
        },
        "checkpoint_id": {
          "type": "string"
        },
        "registry_dir": {
          "type": "string"
        },
        "team": {
          "type": "string"
        },
        "archive": {
          "type": [
            "null",
            "object"
          ],
          "properties": {
            "path": {
              "type": "string"
            },
            "sha256": {
              "type": "string"
            },
            "bytes": {
              "type": "integer"
            },
            "files": {
              "type": "integer"
            }
          },
          "required": [
            "path",
            "sha256",
            "bytes",
            "files"
          ],
          "additionalProperties": false
        }
      },
      "required": [
        "version",
        "workspace_id"
      ],
      "additionalProperties": false
    },
    "bundle": {
      "type": "string"
    },
    "origin_checkpoint_id": {
      "type": "string"
    },
    "error": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "code": {
          "type": "string"
        },
        "phase": {
          "type": "string"
        },
        "attempts": {
          "type": "integer"
        },
        "retryable": {
          "type": "boolean"
        },
        "message": {
          "type": "string"
        }
      },
      "required": [
        "code",
        "phase",
        "attempts",
        "retryable",
        "message"
      ],
      "additionalProperties": false
    }
  },
  "required": [
    "version",
    "outcome"
  ],
  "additionalProperties": false
}
```
## stow_checkpoint_resolve

Workspaces. Resolve a previous capture using its original key and options without creating a new checkpoint.

### Input

```json
{
  "type": "object",
  "properties": {
    "workspace_id": {
      "type": "string"
    },
    "request_key": {
      "type": "string"
    },
    "parent_id": {
      "type": "string"
    },
    "max_bytes": {
      "type": "integer"
    },
    "max_files": {
      "type": "integer"
    }
  },
  "required": [
    "workspace_id",
    "request_key"
  ],
  "additionalProperties": false
}
```

### Result

```json
{
  "type": "object",
  "properties": {
    "version": {
      "type": "integer"
    },
    "outcome": {
      "type": "string"
    },
    "workspace": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "workspace_id": {
          "type": "string"
        },
        "directory": {
          "type": "string"
        },
        "bucket": {
          "type": "string"
        },
        "max_checkpoint_bytes": {
          "type": "integer"
        },
        "max_checkpoints": {
          "type": "integer"
        }
      },
      "required": [
        "workspace_id",
        "directory",
        "bucket",
        "max_checkpoint_bytes",
        "max_checkpoints"
      ],
      "additionalProperties": false
    },
    "capture": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "checkpoint": {
          "type": "object",
          "properties": {
            "version": {
              "type": "integer"
            },
            "objects": {
              "type": "integer"
            },
            "id": {
              "type": "string"
            },
            "workspace_id": {
              "type": "string"
            },
            "parent_id": {
              "type": "string"
            },
            "created": {
              "type": "string"
            },
            "files": {
              "type": "integer"
            },
            "bytes": {
              "type": "integer"
            },
            "excluded": {
              "type": [
                "null",
                "array"
              ],
              "items": {
                "type": "string"
              }
            }
          },
          "required": [
            "id",
            "workspace_id",
            "parent_id",
            "created",
            "files",
            "bytes",
            "excluded"
          ],
          "additionalProperties": false
        },
        "replayed": {
          "type": "boolean"
        },
        "outcome": {
          "type": "string"
        }
      },
      "required": [
        "checkpoint",
        "replayed",
        "outcome"
      ],
      "additionalProperties": false
    },
    "checkpoint_id": {
      "type": "string"
    },
    "entries": {
      "type": [
        "null",
        "array"
      ],
      "items": {
        "type": "object",
        "properties": {
          "object": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "bucket": {
                "type": "string"
              },
              "key": {
                "type": "string"
              },
              "payload": {
                "type": "string"
              },
              "size": {
                "type": "integer"
              },
              "sha256": {
                "type": "string"
              },
              "content_type": {
                "type": "string"
              },
              "metadata": {
                "type": "object",
                "additionalProperties": {
                  "type": "string"
                }
              },
              "checksum_algorithm": {
                "type": "string"
              },
              "checksum_value": {
                "type": "string"
              },
              "checksum_type": {
                "type": "string"
              }
            },
            "required": [
              "bucket",
              "key",
              "payload",
              "size",
              "sha256",
              "content_type"
            ],
            "additionalProperties": false
          },
          "bucket": {
            "type": "string"
          },
          "object_change": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "bucket": {
                "type": "string"
              },
              "key": {
                "type": "string"
              },
              "kind": {
                "type": "string"
              },
              "from": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "bucket": {
                    "type": "string"
                  },
                  "key": {
                    "type": "string"
                  },
                  "payload": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "sha256": {
                    "type": "string"
                  },
                  "content_type": {
                    "type": "string"
                  },
                  "metadata": {
                    "type": "object",
                    "additionalProperties": {
                      "type": "string"
                    }
                  },
                  "checksum_algorithm": {
                    "type": "string"
                  },
                  "checksum_value": {
                    "type": "string"
                  },
                  "checksum_type": {
                    "type": "string"
                  }
                },
                "required": [
                  "bucket",
                  "key",
                  "payload",
                  "size",
                  "sha256",
                  "content_type"
                ],
                "additionalProperties": false
              },
              "to": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "bucket": {
                    "type": "string"
                  },
                  "key": {
                    "type": "string"
                  },
                  "payload": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "sha256": {
                    "type": "string"
                  },
                  "content_type": {
                    "type": "string"
                  },
                  "metadata": {
                    "type": "object",
                    "additionalProperties": {
                      "type": "string"
                    }
                  },
                  "checksum_algorithm": {
                    "type": "string"
                  },
                  "checksum_value": {
                    "type": "string"
                  },
                  "checksum_type": {
                    "type": "string"
                  }
                },
                "required": [
                  "bucket",
                  "key",
                  "payload",
                  "size",
                  "sha256",
                  "content_type"
                ],
                "additionalProperties": false
              }
            },
            "required": [
              "bucket",
              "key",
              "kind"
            ],
            "additionalProperties": false
          },
          "bucket_change": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "bucket": {
                "type": "string"
              },
              "kind": {
                "type": "string"
              }
            },
            "required": [
              "bucket",
              "kind"
            ],
            "additionalProperties": false
          },
          "kind": {
            "type": "string"
          },
          "file": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "path": {
                "type": "string"
              },
              "size": {
                "type": "integer"
              },
              "mode": {
                "type": "integer",
                "minimum": 0,
                "maximum": 4294967295
              },
              "sha256": {
                "type": "string"
              }
            },
            "required": [
              "path",
              "size",
              "mode",
              "sha256"
            ],
            "additionalProperties": false
          },
          "change": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "path": {
                "type": "string"
              },
              "kind": {
                "type": "string"
              },
              "from": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "path": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "mode": {
                    "type": "integer",
                    "minimum": 0,
                    "maximum": 4294967295
                  },
                  "sha256": {
                    "type": "string"
                  }
                },
                "required": [
                  "path",
                  "size",
                  "mode",
                  "sha256"
                ],
                "additionalProperties": false
              },
              "to": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "path": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "mode": {
                    "type": "integer",
                    "minimum": 0,
                    "maximum": 4294967295
                  },
                  "sha256": {
                    "type": "string"
                  }
                },
                "required": [
                  "path",
                  "size",
                  "mode",
                  "sha256"
                ],
                "additionalProperties": false
              }
            },
            "required": [
              "path",
              "kind"
            ],
            "additionalProperties": false
          },
          "excluded": {
            "type": "string"
          }
        },
        "required": [
          "kind"
        ],
        "additionalProperties": false
      }
    },
    "next_cursor": {
      "type": "string"
    },
    "handoff": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "version": {
          "type": "integer"
        },
        "workspace_id": {
          "type": "string"
        },
        "checkpoint_id": {
          "type": "string"
        },
        "registry_dir": {
          "type": "string"
        },
        "team": {
          "type": "string"
        },
        "archive": {
          "type": [
            "null",
            "object"
          ],
          "properties": {
            "path": {
              "type": "string"
            },
            "sha256": {
              "type": "string"
            },
            "bytes": {
              "type": "integer"
            },
            "files": {
              "type": "integer"
            }
          },
          "required": [
            "path",
            "sha256",
            "bytes",
            "files"
          ],
          "additionalProperties": false
        }
      },
      "required": [
        "version",
        "workspace_id"
      ],
      "additionalProperties": false
    },
    "bundle": {
      "type": "string"
    },
    "origin_checkpoint_id": {
      "type": "string"
    },
    "error": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "code": {
          "type": "string"
        },
        "phase": {
          "type": "string"
        },
        "attempts": {
          "type": "integer"
        },
        "retryable": {
          "type": "boolean"
        },
        "message": {
          "type": "string"
        }
      },
      "required": [
        "code",
        "phase",
        "attempts",
        "retryable",
        "message"
      ],
      "additionalProperties": false
    }
  },
  "required": [
    "version",
    "outcome"
  ],
  "additionalProperties": false
}
```
## stow_handoff_adopt

Workspaces. Adopt a named transfer bundle into a new directory under the configured destination root. Receiver grants storage access; no commands run.

### Input

```json
{
  "type": "object",
  "properties": {
    "bundle": {
      "type": "string"
    },
    "destination": {
      "type": "string"
    },
    "expect_sha256": {
      "type": "string"
    }
  },
  "required": [
    "bundle",
    "destination"
  ],
  "additionalProperties": false
}
```

### Result

```json
{
  "type": "object",
  "properties": {
    "version": {
      "type": "integer"
    },
    "outcome": {
      "type": "string"
    },
    "workspace": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "workspace_id": {
          "type": "string"
        },
        "directory": {
          "type": "string"
        },
        "bucket": {
          "type": "string"
        },
        "max_checkpoint_bytes": {
          "type": "integer"
        },
        "max_checkpoints": {
          "type": "integer"
        }
      },
      "required": [
        "workspace_id",
        "directory",
        "bucket",
        "max_checkpoint_bytes",
        "max_checkpoints"
      ],
      "additionalProperties": false
    },
    "capture": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "checkpoint": {
          "type": "object",
          "properties": {
            "version": {
              "type": "integer"
            },
            "objects": {
              "type": "integer"
            },
            "id": {
              "type": "string"
            },
            "workspace_id": {
              "type": "string"
            },
            "parent_id": {
              "type": "string"
            },
            "created": {
              "type": "string"
            },
            "files": {
              "type": "integer"
            },
            "bytes": {
              "type": "integer"
            },
            "excluded": {
              "type": [
                "null",
                "array"
              ],
              "items": {
                "type": "string"
              }
            }
          },
          "required": [
            "id",
            "workspace_id",
            "parent_id",
            "created",
            "files",
            "bytes",
            "excluded"
          ],
          "additionalProperties": false
        },
        "replayed": {
          "type": "boolean"
        },
        "outcome": {
          "type": "string"
        }
      },
      "required": [
        "checkpoint",
        "replayed",
        "outcome"
      ],
      "additionalProperties": false
    },
    "checkpoint_id": {
      "type": "string"
    },
    "entries": {
      "type": [
        "null",
        "array"
      ],
      "items": {
        "type": "object",
        "properties": {
          "object": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "bucket": {
                "type": "string"
              },
              "key": {
                "type": "string"
              },
              "payload": {
                "type": "string"
              },
              "size": {
                "type": "integer"
              },
              "sha256": {
                "type": "string"
              },
              "content_type": {
                "type": "string"
              },
              "metadata": {
                "type": "object",
                "additionalProperties": {
                  "type": "string"
                }
              },
              "checksum_algorithm": {
                "type": "string"
              },
              "checksum_value": {
                "type": "string"
              },
              "checksum_type": {
                "type": "string"
              }
            },
            "required": [
              "bucket",
              "key",
              "payload",
              "size",
              "sha256",
              "content_type"
            ],
            "additionalProperties": false
          },
          "bucket": {
            "type": "string"
          },
          "object_change": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "bucket": {
                "type": "string"
              },
              "key": {
                "type": "string"
              },
              "kind": {
                "type": "string"
              },
              "from": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "bucket": {
                    "type": "string"
                  },
                  "key": {
                    "type": "string"
                  },
                  "payload": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "sha256": {
                    "type": "string"
                  },
                  "content_type": {
                    "type": "string"
                  },
                  "metadata": {
                    "type": "object",
                    "additionalProperties": {
                      "type": "string"
                    }
                  },
                  "checksum_algorithm": {
                    "type": "string"
                  },
                  "checksum_value": {
                    "type": "string"
                  },
                  "checksum_type": {
                    "type": "string"
                  }
                },
                "required": [
                  "bucket",
                  "key",
                  "payload",
                  "size",
                  "sha256",
                  "content_type"
                ],
                "additionalProperties": false
              },
              "to": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "bucket": {
                    "type": "string"
                  },
                  "key": {
                    "type": "string"
                  },
                  "payload": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "sha256": {
                    "type": "string"
                  },
                  "content_type": {
                    "type": "string"
                  },
                  "metadata": {
                    "type": "object",
                    "additionalProperties": {
                      "type": "string"
                    }
                  },
                  "checksum_algorithm": {
                    "type": "string"
                  },
                  "checksum_value": {
                    "type": "string"
                  },
                  "checksum_type": {
                    "type": "string"
                  }
                },
                "required": [
                  "bucket",
                  "key",
                  "payload",
                  "size",
                  "sha256",
                  "content_type"
                ],
                "additionalProperties": false
              }
            },
            "required": [
              "bucket",
              "key",
              "kind"
            ],
            "additionalProperties": false
          },
          "bucket_change": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "bucket": {
                "type": "string"
              },
              "kind": {
                "type": "string"
              }
            },
            "required": [
              "bucket",
              "kind"
            ],
            "additionalProperties": false
          },
          "kind": {
            "type": "string"
          },
          "file": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "path": {
                "type": "string"
              },
              "size": {
                "type": "integer"
              },
              "mode": {
                "type": "integer",
                "minimum": 0,
                "maximum": 4294967295
              },
              "sha256": {
                "type": "string"
              }
            },
            "required": [
              "path",
              "size",
              "mode",
              "sha256"
            ],
            "additionalProperties": false
          },
          "change": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "path": {
                "type": "string"
              },
              "kind": {
                "type": "string"
              },
              "from": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "path": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "mode": {
                    "type": "integer",
                    "minimum": 0,
                    "maximum": 4294967295
                  },
                  "sha256": {
                    "type": "string"
                  }
                },
                "required": [
                  "path",
                  "size",
                  "mode",
                  "sha256"
                ],
                "additionalProperties": false
              },
              "to": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "path": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "mode": {
                    "type": "integer",
                    "minimum": 0,
                    "maximum": 4294967295
                  },
                  "sha256": {
                    "type": "string"
                  }
                },
                "required": [
                  "path",
                  "size",
                  "mode",
                  "sha256"
                ],
                "additionalProperties": false
              }
            },
            "required": [
              "path",
              "kind"
            ],
            "additionalProperties": false
          },
          "excluded": {
            "type": "string"
          }
        },
        "required": [
          "kind"
        ],
        "additionalProperties": false
      }
    },
    "next_cursor": {
      "type": "string"
    },
    "handoff": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "version": {
          "type": "integer"
        },
        "workspace_id": {
          "type": "string"
        },
        "checkpoint_id": {
          "type": "string"
        },
        "registry_dir": {
          "type": "string"
        },
        "team": {
          "type": "string"
        },
        "archive": {
          "type": [
            "null",
            "object"
          ],
          "properties": {
            "path": {
              "type": "string"
            },
            "sha256": {
              "type": "string"
            },
            "bytes": {
              "type": "integer"
            },
            "files": {
              "type": "integer"
            }
          },
          "required": [
            "path",
            "sha256",
            "bytes",
            "files"
          ],
          "additionalProperties": false
        }
      },
      "required": [
        "version",
        "workspace_id"
      ],
      "additionalProperties": false
    },
    "bundle": {
      "type": "string"
    },
    "origin_checkpoint_id": {
      "type": "string"
    },
    "error": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "code": {
          "type": "string"
        },
        "phase": {
          "type": "string"
        },
        "attempts": {
          "type": "integer"
        },
        "retryable": {
          "type": "boolean"
        },
        "message": {
          "type": "string"
        }
      },
      "required": [
        "code",
        "phase",
        "attempts",
        "retryable",
        "message"
      ],
      "additionalProperties": false
    }
  },
  "required": [
    "version",
    "outcome"
  ],
  "additionalProperties": false
}
```
## stow_handoff_export

Workspaces. Export a checkpoint to a new named bundle in the configured transfer directory. No automatic retries.

### Input

```json
{
  "type": "object",
  "properties": {
    "workspace_id": {
      "type": "string"
    },
    "checkpoint_id": {
      "type": "string"
    },
    "bundle": {
      "type": "string"
    }
  },
  "required": [
    "workspace_id",
    "checkpoint_id",
    "bundle"
  ],
  "additionalProperties": false
}
```

### Result

```json
{
  "type": "object",
  "properties": {
    "version": {
      "type": "integer"
    },
    "outcome": {
      "type": "string"
    },
    "workspace": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "workspace_id": {
          "type": "string"
        },
        "directory": {
          "type": "string"
        },
        "bucket": {
          "type": "string"
        },
        "max_checkpoint_bytes": {
          "type": "integer"
        },
        "max_checkpoints": {
          "type": "integer"
        }
      },
      "required": [
        "workspace_id",
        "directory",
        "bucket",
        "max_checkpoint_bytes",
        "max_checkpoints"
      ],
      "additionalProperties": false
    },
    "capture": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "checkpoint": {
          "type": "object",
          "properties": {
            "version": {
              "type": "integer"
            },
            "objects": {
              "type": "integer"
            },
            "id": {
              "type": "string"
            },
            "workspace_id": {
              "type": "string"
            },
            "parent_id": {
              "type": "string"
            },
            "created": {
              "type": "string"
            },
            "files": {
              "type": "integer"
            },
            "bytes": {
              "type": "integer"
            },
            "excluded": {
              "type": [
                "null",
                "array"
              ],
              "items": {
                "type": "string"
              }
            }
          },
          "required": [
            "id",
            "workspace_id",
            "parent_id",
            "created",
            "files",
            "bytes",
            "excluded"
          ],
          "additionalProperties": false
        },
        "replayed": {
          "type": "boolean"
        },
        "outcome": {
          "type": "string"
        }
      },
      "required": [
        "checkpoint",
        "replayed",
        "outcome"
      ],
      "additionalProperties": false
    },
    "checkpoint_id": {
      "type": "string"
    },
    "entries": {
      "type": [
        "null",
        "array"
      ],
      "items": {
        "type": "object",
        "properties": {
          "object": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "bucket": {
                "type": "string"
              },
              "key": {
                "type": "string"
              },
              "payload": {
                "type": "string"
              },
              "size": {
                "type": "integer"
              },
              "sha256": {
                "type": "string"
              },
              "content_type": {
                "type": "string"
              },
              "metadata": {
                "type": "object",
                "additionalProperties": {
                  "type": "string"
                }
              },
              "checksum_algorithm": {
                "type": "string"
              },
              "checksum_value": {
                "type": "string"
              },
              "checksum_type": {
                "type": "string"
              }
            },
            "required": [
              "bucket",
              "key",
              "payload",
              "size",
              "sha256",
              "content_type"
            ],
            "additionalProperties": false
          },
          "bucket": {
            "type": "string"
          },
          "object_change": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "bucket": {
                "type": "string"
              },
              "key": {
                "type": "string"
              },
              "kind": {
                "type": "string"
              },
              "from": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "bucket": {
                    "type": "string"
                  },
                  "key": {
                    "type": "string"
                  },
                  "payload": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "sha256": {
                    "type": "string"
                  },
                  "content_type": {
                    "type": "string"
                  },
                  "metadata": {
                    "type": "object",
                    "additionalProperties": {
                      "type": "string"
                    }
                  },
                  "checksum_algorithm": {
                    "type": "string"
                  },
                  "checksum_value": {
                    "type": "string"
                  },
                  "checksum_type": {
                    "type": "string"
                  }
                },
                "required": [
                  "bucket",
                  "key",
                  "payload",
                  "size",
                  "sha256",
                  "content_type"
                ],
                "additionalProperties": false
              },
              "to": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "bucket": {
                    "type": "string"
                  },
                  "key": {
                    "type": "string"
                  },
                  "payload": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "sha256": {
                    "type": "string"
                  },
                  "content_type": {
                    "type": "string"
                  },
                  "metadata": {
                    "type": "object",
                    "additionalProperties": {
                      "type": "string"
                    }
                  },
                  "checksum_algorithm": {
                    "type": "string"
                  },
                  "checksum_value": {
                    "type": "string"
                  },
                  "checksum_type": {
                    "type": "string"
                  }
                },
                "required": [
                  "bucket",
                  "key",
                  "payload",
                  "size",
                  "sha256",
                  "content_type"
                ],
                "additionalProperties": false
              }
            },
            "required": [
              "bucket",
              "key",
              "kind"
            ],
            "additionalProperties": false
          },
          "bucket_change": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "bucket": {
                "type": "string"
              },
              "kind": {
                "type": "string"
              }
            },
            "required": [
              "bucket",
              "kind"
            ],
            "additionalProperties": false
          },
          "kind": {
            "type": "string"
          },
          "file": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "path": {
                "type": "string"
              },
              "size": {
                "type": "integer"
              },
              "mode": {
                "type": "integer",
                "minimum": 0,
                "maximum": 4294967295
              },
              "sha256": {
                "type": "string"
              }
            },
            "required": [
              "path",
              "size",
              "mode",
              "sha256"
            ],
            "additionalProperties": false
          },
          "change": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "path": {
                "type": "string"
              },
              "kind": {
                "type": "string"
              },
              "from": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "path": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "mode": {
                    "type": "integer",
                    "minimum": 0,
                    "maximum": 4294967295
                  },
                  "sha256": {
                    "type": "string"
                  }
                },
                "required": [
                  "path",
                  "size",
                  "mode",
                  "sha256"
                ],
                "additionalProperties": false
              },
              "to": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "path": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "mode": {
                    "type": "integer",
                    "minimum": 0,
                    "maximum": 4294967295
                  },
                  "sha256": {
                    "type": "string"
                  }
                },
                "required": [
                  "path",
                  "size",
                  "mode",
                  "sha256"
                ],
                "additionalProperties": false
              }
            },
            "required": [
              "path",
              "kind"
            ],
            "additionalProperties": false
          },
          "excluded": {
            "type": "string"
          }
        },
        "required": [
          "kind"
        ],
        "additionalProperties": false
      }
    },
    "next_cursor": {
      "type": "string"
    },
    "handoff": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "version": {
          "type": "integer"
        },
        "workspace_id": {
          "type": "string"
        },
        "checkpoint_id": {
          "type": "string"
        },
        "registry_dir": {
          "type": "string"
        },
        "team": {
          "type": "string"
        },
        "archive": {
          "type": [
            "null",
            "object"
          ],
          "properties": {
            "path": {
              "type": "string"
            },
            "sha256": {
              "type": "string"
            },
            "bytes": {
              "type": "integer"
            },
            "files": {
              "type": "integer"
            }
          },
          "required": [
            "path",
            "sha256",
            "bytes",
            "files"
          ],
          "additionalProperties": false
        }
      },
      "required": [
        "version",
        "workspace_id"
      ],
      "additionalProperties": false
    },
    "bundle": {
      "type": "string"
    },
    "origin_checkpoint_id": {
      "type": "string"
    },
    "error": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "code": {
          "type": "string"
        },
        "phase": {
          "type": "string"
        },
        "attempts": {
          "type": "integer"
        },
        "retryable": {
          "type": "boolean"
        },
        "message": {
          "type": "string"
        }
      },
      "required": [
        "code",
        "phase",
        "attempts",
        "retryable",
        "message"
      ],
      "additionalProperties": false
    }
  },
  "required": [
    "version",
    "outcome"
  ],
  "additionalProperties": false
}
```
## stow_object_capabilities

Object storage. Inspect the configured object profile and adapter bounds without disclosing host paths.

### Input

```json
{
  "type": "object",
  "additionalProperties": false
}
```

### Result

```json
{
  "type": "object",
  "properties": {
    "version": {
      "type": "integer"
    },
    "outcome": {
      "type": "string"
    },
    "available": {
      "type": "boolean"
    },
    "object": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "key": {
          "type": "string"
        },
        "size": {
          "type": "integer"
        },
        "etag": {
          "type": "string"
        },
        "content_type": {
          "type": "string"
        },
        "metadata": {
          "type": "object",
          "additionalProperties": {
            "type": "string"
          }
        },
        "last_modified": {
          "type": "string"
        },
        "data_base64": {
          "type": "string"
        }
      },
      "required": [
        "key",
        "size",
        "etag",
        "last_modified"
      ],
      "additionalProperties": false
    },
    "observation_token": {
      "type": "string"
    },
    "absent": {
      "type": "boolean"
    },
    "replayed": {
      "type": "boolean"
    },
    "released": {
      "type": "boolean"
    },
    "capabilities": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "backend": {
          "type": "string"
        },
        "persistent": {
          "type": "boolean"
        },
        "guarded_saves": {
          "type": "boolean"
        },
        "durable_save_requests": {
          "type": "boolean"
        },
        "read_only": {
          "type": "boolean"
        },
        "max_object_bytes": {
          "type": "integer"
        },
        "max_observations": {
          "type": "integer"
        },
        "observation_ttl_seconds": {
          "type": "integer"
        },
        "max_response_bytes": {
          "type": "integer"
        },
        "max_tool_arguments_bytes": {
          "type": "integer"
        },
        "max_concurrent_calls": {
          "type": "integer"
        }
      },
      "required": [
        "backend",
        "persistent",
        "guarded_saves",
        "durable_save_requests",
        "read_only",
        "max_object_bytes",
        "max_observations",
        "observation_ttl_seconds",
        "max_response_bytes",
        "max_tool_arguments_bytes",
        "max_concurrent_calls"
      ],
      "additionalProperties": false
    },
    "error": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "code": {
          "type": "string"
        },
        "message": {
          "type": "string"
        }
      },
      "required": [
        "code",
        "message"
      ],
      "additionalProperties": false
    }
  },
  "required": [
    "version",
    "outcome",
    "available",
    "absent",
    "replayed"
  ],
  "additionalProperties": false
}
```
## stow_object_mentions

Object storage. Find up to 50 object references by key prefix in the configured bucket.

### Input

```json
{
  "type": "object",
  "properties": {
    "query": {
      "type": "string"
    }
  },
  "required": [
    "query"
  ],
  "additionalProperties": false
}
```

### Result

```json
{
  "type": "object",
  "properties": {
    "items": {
      "type": [
        "null",
        "array"
      ],
      "items": {
        "type": "object",
        "properties": {
          "type": {
            "type": "string"
          },
          "uri": {
            "type": "string"
          },
          "name": {
            "type": "string"
          },
          "mimeType": {
            "type": "string"
          },
          "size": {
            "type": "integer"
          }
        },
        "required": [
          "type",
          "uri",
          "name",
          "mimeType",
          "size"
        ],
        "additionalProperties": false
      }
    }
  },
  "required": [
    "items"
  ],
  "additionalProperties": false
}
```
## stow_object_read_for_save

Object storage. Read bounded bytes and an opaque live comparison token, including observed absence.

### Input

```json
{
  "type": "object",
  "properties": {
    "key": {
      "type": "string"
    }
  },
  "required": [
    "key"
  ],
  "additionalProperties": false
}
```

### Result

```json
{
  "type": "object",
  "properties": {
    "version": {
      "type": "integer"
    },
    "outcome": {
      "type": "string"
    },
    "available": {
      "type": "boolean"
    },
    "object": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "key": {
          "type": "string"
        },
        "size": {
          "type": "integer"
        },
        "etag": {
          "type": "string"
        },
        "content_type": {
          "type": "string"
        },
        "metadata": {
          "type": "object",
          "additionalProperties": {
            "type": "string"
          }
        },
        "last_modified": {
          "type": "string"
        },
        "data_base64": {
          "type": "string"
        }
      },
      "required": [
        "key",
        "size",
        "etag",
        "last_modified"
      ],
      "additionalProperties": false
    },
    "observation_token": {
      "type": "string"
    },
    "absent": {
      "type": "boolean"
    },
    "replayed": {
      "type": "boolean"
    },
    "released": {
      "type": "boolean"
    },
    "capabilities": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "backend": {
          "type": "string"
        },
        "persistent": {
          "type": "boolean"
        },
        "guarded_saves": {
          "type": "boolean"
        },
        "durable_save_requests": {
          "type": "boolean"
        },
        "read_only": {
          "type": "boolean"
        },
        "max_object_bytes": {
          "type": "integer"
        },
        "max_observations": {
          "type": "integer"
        },
        "observation_ttl_seconds": {
          "type": "integer"
        },
        "max_response_bytes": {
          "type": "integer"
        },
        "max_tool_arguments_bytes": {
          "type": "integer"
        },
        "max_concurrent_calls": {
          "type": "integer"
        }
      },
      "required": [
        "backend",
        "persistent",
        "guarded_saves",
        "durable_save_requests",
        "read_only",
        "max_object_bytes",
        "max_observations",
        "observation_ttl_seconds",
        "max_response_bytes",
        "max_tool_arguments_bytes",
        "max_concurrent_calls"
      ],
      "additionalProperties": false
    },
    "error": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "code": {
          "type": "string"
        },
        "message": {
          "type": "string"
        }
      },
      "required": [
        "code",
        "message"
      ],
      "additionalProperties": false
    }
  },
  "required": [
    "version",
    "outcome",
    "available",
    "absent",
    "replayed"
  ],
  "additionalProperties": false
}
```
## stow_object_release_observation

Object storage. Release a volatile read observation. Repeating release is safe; receipts remain retained.

### Input

```json
{
  "type": "object",
  "properties": {
    "observation_token": {
      "type": "string"
    }
  },
  "required": [
    "observation_token"
  ],
  "additionalProperties": false
}
```

### Result

```json
{
  "type": "object",
  "properties": {
    "version": {
      "type": "integer"
    },
    "outcome": {
      "type": "string"
    },
    "available": {
      "type": "boolean"
    },
    "object": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "key": {
          "type": "string"
        },
        "size": {
          "type": "integer"
        },
        "etag": {
          "type": "string"
        },
        "content_type": {
          "type": "string"
        },
        "metadata": {
          "type": "object",
          "additionalProperties": {
            "type": "string"
          }
        },
        "last_modified": {
          "type": "string"
        },
        "data_base64": {
          "type": "string"
        }
      },
      "required": [
        "key",
        "size",
        "etag",
        "last_modified"
      ],
      "additionalProperties": false
    },
    "observation_token": {
      "type": "string"
    },
    "absent": {
      "type": "boolean"
    },
    "replayed": {
      "type": "boolean"
    },
    "released": {
      "type": "boolean"
    },
    "capabilities": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "backend": {
          "type": "string"
        },
        "persistent": {
          "type": "boolean"
        },
        "guarded_saves": {
          "type": "boolean"
        },
        "durable_save_requests": {
          "type": "boolean"
        },
        "read_only": {
          "type": "boolean"
        },
        "max_object_bytes": {
          "type": "integer"
        },
        "max_observations": {
          "type": "integer"
        },
        "observation_ttl_seconds": {
          "type": "integer"
        },
        "max_response_bytes": {
          "type": "integer"
        },
        "max_tool_arguments_bytes": {
          "type": "integer"
        },
        "max_concurrent_calls": {
          "type": "integer"
        }
      },
      "required": [
        "backend",
        "persistent",
        "guarded_saves",
        "durable_save_requests",
        "read_only",
        "max_object_bytes",
        "max_observations",
        "observation_ttl_seconds",
        "max_response_bytes",
        "max_tool_arguments_bytes",
        "max_concurrent_calls"
      ],
      "additionalProperties": false
    },
    "error": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "code": {
          "type": "string"
        },
        "message": {
          "type": "string"
        }
      },
      "required": [
        "code",
        "message"
      ],
      "additionalProperties": false
    }
  },
  "required": [
    "version",
    "outcome",
    "available",
    "absent",
    "replayed"
  ],
  "additionalProperties": false
}
```
## stow_object_resolve_save

Object storage. Resolve the original retained request after interruption or reopen without starting a new save.

### Input

```json
{
  "type": "object",
  "properties": {
    "key": {
      "type": "string"
    },
    "request_key": {
      "type": "string"
    }
  },
  "required": [
    "key",
    "request_key"
  ],
  "additionalProperties": false
}
```

### Result

```json
{
  "type": "object",
  "properties": {
    "version": {
      "type": "integer"
    },
    "outcome": {
      "type": "string"
    },
    "available": {
      "type": "boolean"
    },
    "object": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "key": {
          "type": "string"
        },
        "size": {
          "type": "integer"
        },
        "etag": {
          "type": "string"
        },
        "content_type": {
          "type": "string"
        },
        "metadata": {
          "type": "object",
          "additionalProperties": {
            "type": "string"
          }
        },
        "last_modified": {
          "type": "string"
        },
        "data_base64": {
          "type": "string"
        }
      },
      "required": [
        "key",
        "size",
        "etag",
        "last_modified"
      ],
      "additionalProperties": false
    },
    "observation_token": {
      "type": "string"
    },
    "absent": {
      "type": "boolean"
    },
    "replayed": {
      "type": "boolean"
    },
    "released": {
      "type": "boolean"
    },
    "capabilities": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "backend": {
          "type": "string"
        },
        "persistent": {
          "type": "boolean"
        },
        "guarded_saves": {
          "type": "boolean"
        },
        "durable_save_requests": {
          "type": "boolean"
        },
        "read_only": {
          "type": "boolean"
        },
        "max_object_bytes": {
          "type": "integer"
        },
        "max_observations": {
          "type": "integer"
        },
        "observation_ttl_seconds": {
          "type": "integer"
        },
        "max_response_bytes": {
          "type": "integer"
        },
        "max_tool_arguments_bytes": {
          "type": "integer"
        },
        "max_concurrent_calls": {
          "type": "integer"
        }
      },
      "required": [
        "backend",
        "persistent",
        "guarded_saves",
        "durable_save_requests",
        "read_only",
        "max_object_bytes",
        "max_observations",
        "observation_ttl_seconds",
        "max_response_bytes",
        "max_tool_arguments_bytes",
        "max_concurrent_calls"
      ],
      "additionalProperties": false
    },
    "error": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "code": {
          "type": "string"
        },
        "message": {
          "type": "string"
        }
      },
      "required": [
        "code",
        "message"
      ],
      "additionalProperties": false
    }
  },
  "required": [
    "version",
    "outcome",
    "available",
    "absent",
    "replayed"
  ],
  "additionalProperties": false
}
```
## stow_object_save

Object storage. Save with a live observation or deliberate replacement and persisted request key. Preserve original inputs on retries.

### Input

```json
{
  "type": "object",
  "properties": {
    "key": {
      "type": "string"
    },
    "data_base64": {
      "type": "string"
    },
    "content_type": {
      "type": "string"
    },
    "metadata": {
      "type": "object",
      "additionalProperties": {
        "type": "string"
      }
    },
    "observation_token": {
      "type": "string"
    },
    "replace": {
      "type": "boolean"
    },
    "request_key": {
      "type": "string"
    }
  },
  "required": [
    "key",
    "data_base64",
    "request_key"
  ],
  "additionalProperties": false
}
```

### Result

```json
{
  "type": "object",
  "properties": {
    "version": {
      "type": "integer"
    },
    "outcome": {
      "type": "string"
    },
    "available": {
      "type": "boolean"
    },
    "object": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "key": {
          "type": "string"
        },
        "size": {
          "type": "integer"
        },
        "etag": {
          "type": "string"
        },
        "content_type": {
          "type": "string"
        },
        "metadata": {
          "type": "object",
          "additionalProperties": {
            "type": "string"
          }
        },
        "last_modified": {
          "type": "string"
        },
        "data_base64": {
          "type": "string"
        }
      },
      "required": [
        "key",
        "size",
        "etag",
        "last_modified"
      ],
      "additionalProperties": false
    },
    "observation_token": {
      "type": "string"
    },
    "absent": {
      "type": "boolean"
    },
    "replayed": {
      "type": "boolean"
    },
    "released": {
      "type": "boolean"
    },
    "capabilities": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "backend": {
          "type": "string"
        },
        "persistent": {
          "type": "boolean"
        },
        "guarded_saves": {
          "type": "boolean"
        },
        "durable_save_requests": {
          "type": "boolean"
        },
        "read_only": {
          "type": "boolean"
        },
        "max_object_bytes": {
          "type": "integer"
        },
        "max_observations": {
          "type": "integer"
        },
        "observation_ttl_seconds": {
          "type": "integer"
        },
        "max_response_bytes": {
          "type": "integer"
        },
        "max_tool_arguments_bytes": {
          "type": "integer"
        },
        "max_concurrent_calls": {
          "type": "integer"
        }
      },
      "required": [
        "backend",
        "persistent",
        "guarded_saves",
        "durable_save_requests",
        "read_only",
        "max_object_bytes",
        "max_observations",
        "observation_ttl_seconds",
        "max_response_bytes",
        "max_tool_arguments_bytes",
        "max_concurrent_calls"
      ],
      "additionalProperties": false
    },
    "error": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "code": {
          "type": "string"
        },
        "message": {
          "type": "string"
        }
      },
      "required": [
        "code",
        "message"
      ],
      "additionalProperties": false
    }
  },
  "required": [
    "version",
    "outcome",
    "available",
    "absent",
    "replayed"
  ],
  "additionalProperties": false
}
```
## stow_workspace_browser

Workspace browser (opt-in). Browse the configured workspace through bounded read-only resources.

### Input

```json
{
  "type": "object",
  "additionalProperties": false
}
```

### Result

```json
{
  "type": "object",
  "properties": {
    "mode": {
      "type": "string"
    },
    "overview_uri": {
      "type": "string"
    }
  },
  "required": [
    "mode",
    "overview_uri"
  ],
  "additionalProperties": false
}
```
## stow_workspace_file_read_for_save

Workspace file editing (opt-in). Read one bounded workspace file and receive a volatile observation token for a guarded save.

### Input

```json
{
  "type": "object",
  "properties": {
    "path": {
      "type": "string"
    }
  },
  "required": [
    "path"
  ],
  "additionalProperties": false
}
```

### Result

```json
{
  "type": "object",
  "properties": {
    "version": {
      "type": "integer"
    },
    "outcome": {
      "type": "string"
    },
    "available": {
      "type": "boolean"
    },
    "path": {
      "type": "string"
    },
    "absent": {
      "type": "boolean"
    },
    "data_base64": {
      "type": [
        "null",
        "string"
      ]
    },
    "text": {
      "type": [
        "null",
        "string"
      ]
    },
    "sha256": {
      "type": "string"
    },
    "mode": {
      "type": [
        "null",
        "integer"
      ],
      "minimum": 0,
      "maximum": 4294967295
    },
    "modified": {
      "type": "string"
    },
    "generation": {
      "type": "string"
    },
    "observation_token": {
      "type": "string"
    },
    "expires_at": {
      "type": "string"
    },
    "replayed": {
      "type": "boolean"
    },
    "committed": {
      "type": "boolean"
    },
    "released": {
      "type": "boolean"
    },
    "object": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "path": {
          "type": "string"
        },
        "size": {
          "type": "integer"
        },
        "etag": {
          "type": "string"
        },
        "content_type": {
          "type": "string"
        },
        "metadata": {
          "type": "object",
          "additionalProperties": {
            "type": "string"
          }
        },
        "last_modified": {
          "type": "string"
        }
      },
      "required": [
        "path",
        "size",
        "etag",
        "last_modified"
      ],
      "additionalProperties": false
    },
    "error": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "code": {
          "type": "string"
        },
        "message": {
          "type": "string"
        }
      },
      "required": [
        "code",
        "message"
      ],
      "additionalProperties": false
    }
  },
  "required": [
    "version",
    "outcome",
    "available",
    "absent"
  ],
  "additionalProperties": false
}
```
## stow_workspace_file_release_observation

Workspace file editing (opt-in). Release a volatile workspace file observation; saved receipts remain retained.

### Input

```json
{
  "type": "object",
  "properties": {
    "observation_token": {
      "type": "string"
    }
  },
  "required": [
    "observation_token"
  ],
  "additionalProperties": false
}
```

### Result

```json
{
  "type": "object",
  "properties": {
    "version": {
      "type": "integer"
    },
    "outcome": {
      "type": "string"
    },
    "available": {
      "type": "boolean"
    },
    "path": {
      "type": "string"
    },
    "absent": {
      "type": "boolean"
    },
    "data_base64": {
      "type": [
        "null",
        "string"
      ]
    },
    "text": {
      "type": [
        "null",
        "string"
      ]
    },
    "sha256": {
      "type": "string"
    },
    "mode": {
      "type": [
        "null",
        "integer"
      ],
      "minimum": 0,
      "maximum": 4294967295
    },
    "modified": {
      "type": "string"
    },
    "generation": {
      "type": "string"
    },
    "observation_token": {
      "type": "string"
    },
    "expires_at": {
      "type": "string"
    },
    "replayed": {
      "type": "boolean"
    },
    "committed": {
      "type": "boolean"
    },
    "released": {
      "type": "boolean"
    },
    "object": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "path": {
          "type": "string"
        },
        "size": {
          "type": "integer"
        },
        "etag": {
          "type": "string"
        },
        "content_type": {
          "type": "string"
        },
        "metadata": {
          "type": "object",
          "additionalProperties": {
            "type": "string"
          }
        },
        "last_modified": {
          "type": "string"
        }
      },
      "required": [
        "path",
        "size",
        "etag",
        "last_modified"
      ],
      "additionalProperties": false
    },
    "error": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "code": {
          "type": "string"
        },
        "message": {
          "type": "string"
        }
      },
      "required": [
        "code",
        "message"
      ],
      "additionalProperties": false
    }
  },
  "required": [
    "version",
    "outcome",
    "available",
    "absent"
  ],
  "additionalProperties": false
}
```
## stow_workspace_file_resolve_save

Workspace file editing (opt-in). Resolve the retained result for the original workspace file save request.

### Input

```json
{
  "type": "object",
  "properties": {
    "path": {
      "type": "string"
    },
    "request_key": {
      "type": "string"
    }
  },
  "required": [
    "path",
    "request_key"
  ],
  "additionalProperties": false
}
```

### Result

```json
{
  "type": "object",
  "properties": {
    "version": {
      "type": "integer"
    },
    "outcome": {
      "type": "string"
    },
    "available": {
      "type": "boolean"
    },
    "path": {
      "type": "string"
    },
    "absent": {
      "type": "boolean"
    },
    "data_base64": {
      "type": [
        "null",
        "string"
      ]
    },
    "text": {
      "type": [
        "null",
        "string"
      ]
    },
    "sha256": {
      "type": "string"
    },
    "mode": {
      "type": [
        "null",
        "integer"
      ],
      "minimum": 0,
      "maximum": 4294967295
    },
    "modified": {
      "type": "string"
    },
    "generation": {
      "type": "string"
    },
    "observation_token": {
      "type": "string"
    },
    "expires_at": {
      "type": "string"
    },
    "replayed": {
      "type": "boolean"
    },
    "committed": {
      "type": "boolean"
    },
    "released": {
      "type": "boolean"
    },
    "object": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "path": {
          "type": "string"
        },
        "size": {
          "type": "integer"
        },
        "etag": {
          "type": "string"
        },
        "content_type": {
          "type": "string"
        },
        "metadata": {
          "type": "object",
          "additionalProperties": {
            "type": "string"
          }
        },
        "last_modified": {
          "type": "string"
        }
      },
      "required": [
        "path",
        "size",
        "etag",
        "last_modified"
      ],
      "additionalProperties": false
    },
    "error": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "code": {
          "type": "string"
        },
        "message": {
          "type": "string"
        }
      },
      "required": [
        "code",
        "message"
      ],
      "additionalProperties": false
    }
  },
  "required": [
    "version",
    "outcome",
    "available",
    "absent"
  ],
  "additionalProperties": false
}
```
## stow_workspace_file_save

Workspace file editing (opt-in). Save bounded text or base64 bytes using the observed token or deliberate replacement and a persistent request key.

### Input

```json
{
  "type": "object",
  "properties": {
    "path": {
      "type": "string"
    },
    "text": {
      "type": [
        "null",
        "string"
      ]
    },
    "data_base64": {
      "type": [
        "null",
        "string"
      ]
    },
    "request_key": {
      "type": "string"
    },
    "observation_token": {
      "type": "string"
    },
    "replace": {
      "type": "boolean"
    }
  },
  "required": [
    "path",
    "request_key"
  ],
  "additionalProperties": false
}
```

### Result

```json
{
  "type": "object",
  "properties": {
    "version": {
      "type": "integer"
    },
    "outcome": {
      "type": "string"
    },
    "available": {
      "type": "boolean"
    },
    "path": {
      "type": "string"
    },
    "absent": {
      "type": "boolean"
    },
    "data_base64": {
      "type": [
        "null",
        "string"
      ]
    },
    "text": {
      "type": [
        "null",
        "string"
      ]
    },
    "sha256": {
      "type": "string"
    },
    "mode": {
      "type": [
        "null",
        "integer"
      ],
      "minimum": 0,
      "maximum": 4294967295
    },
    "modified": {
      "type": "string"
    },
    "generation": {
      "type": "string"
    },
    "observation_token": {
      "type": "string"
    },
    "expires_at": {
      "type": "string"
    },
    "replayed": {
      "type": "boolean"
    },
    "committed": {
      "type": "boolean"
    },
    "released": {
      "type": "boolean"
    },
    "object": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "path": {
          "type": "string"
        },
        "size": {
          "type": "integer"
        },
        "etag": {
          "type": "string"
        },
        "content_type": {
          "type": "string"
        },
        "metadata": {
          "type": "object",
          "additionalProperties": {
            "type": "string"
          }
        },
        "last_modified": {
          "type": "string"
        }
      },
      "required": [
        "path",
        "size",
        "etag",
        "last_modified"
      ],
      "additionalProperties": false
    },
    "error": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "code": {
          "type": "string"
        },
        "message": {
          "type": "string"
        }
      },
      "required": [
        "code",
        "message"
      ],
      "additionalProperties": false
    }
  },
  "required": [
    "version",
    "outcome",
    "available",
    "absent"
  ],
  "additionalProperties": false
}
```
## stow_workspace_inspect

Workspaces. Inspect the configured workspace without opening or claiming it.

### Input

```json
{
  "type": "object",
  "properties": {
    "workspace_id": {
      "type": "string"
    }
  },
  "required": [
    "workspace_id"
  ],
  "additionalProperties": false
}
```

### Result

```json
{
  "type": "object",
  "properties": {
    "version": {
      "type": "integer"
    },
    "outcome": {
      "type": "string"
    },
    "workspace": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "workspace_id": {
          "type": "string"
        },
        "directory": {
          "type": "string"
        },
        "bucket": {
          "type": "string"
        },
        "max_checkpoint_bytes": {
          "type": "integer"
        },
        "max_checkpoints": {
          "type": "integer"
        }
      },
      "required": [
        "workspace_id",
        "directory",
        "bucket",
        "max_checkpoint_bytes",
        "max_checkpoints"
      ],
      "additionalProperties": false
    },
    "capture": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "checkpoint": {
          "type": "object",
          "properties": {
            "version": {
              "type": "integer"
            },
            "objects": {
              "type": "integer"
            },
            "id": {
              "type": "string"
            },
            "workspace_id": {
              "type": "string"
            },
            "parent_id": {
              "type": "string"
            },
            "created": {
              "type": "string"
            },
            "files": {
              "type": "integer"
            },
            "bytes": {
              "type": "integer"
            },
            "excluded": {
              "type": [
                "null",
                "array"
              ],
              "items": {
                "type": "string"
              }
            }
          },
          "required": [
            "id",
            "workspace_id",
            "parent_id",
            "created",
            "files",
            "bytes",
            "excluded"
          ],
          "additionalProperties": false
        },
        "replayed": {
          "type": "boolean"
        },
        "outcome": {
          "type": "string"
        }
      },
      "required": [
        "checkpoint",
        "replayed",
        "outcome"
      ],
      "additionalProperties": false
    },
    "checkpoint_id": {
      "type": "string"
    },
    "entries": {
      "type": [
        "null",
        "array"
      ],
      "items": {
        "type": "object",
        "properties": {
          "object": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "bucket": {
                "type": "string"
              },
              "key": {
                "type": "string"
              },
              "payload": {
                "type": "string"
              },
              "size": {
                "type": "integer"
              },
              "sha256": {
                "type": "string"
              },
              "content_type": {
                "type": "string"
              },
              "metadata": {
                "type": "object",
                "additionalProperties": {
                  "type": "string"
                }
              },
              "checksum_algorithm": {
                "type": "string"
              },
              "checksum_value": {
                "type": "string"
              },
              "checksum_type": {
                "type": "string"
              }
            },
            "required": [
              "bucket",
              "key",
              "payload",
              "size",
              "sha256",
              "content_type"
            ],
            "additionalProperties": false
          },
          "bucket": {
            "type": "string"
          },
          "object_change": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "bucket": {
                "type": "string"
              },
              "key": {
                "type": "string"
              },
              "kind": {
                "type": "string"
              },
              "from": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "bucket": {
                    "type": "string"
                  },
                  "key": {
                    "type": "string"
                  },
                  "payload": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "sha256": {
                    "type": "string"
                  },
                  "content_type": {
                    "type": "string"
                  },
                  "metadata": {
                    "type": "object",
                    "additionalProperties": {
                      "type": "string"
                    }
                  },
                  "checksum_algorithm": {
                    "type": "string"
                  },
                  "checksum_value": {
                    "type": "string"
                  },
                  "checksum_type": {
                    "type": "string"
                  }
                },
                "required": [
                  "bucket",
                  "key",
                  "payload",
                  "size",
                  "sha256",
                  "content_type"
                ],
                "additionalProperties": false
              },
              "to": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "bucket": {
                    "type": "string"
                  },
                  "key": {
                    "type": "string"
                  },
                  "payload": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "sha256": {
                    "type": "string"
                  },
                  "content_type": {
                    "type": "string"
                  },
                  "metadata": {
                    "type": "object",
                    "additionalProperties": {
                      "type": "string"
                    }
                  },
                  "checksum_algorithm": {
                    "type": "string"
                  },
                  "checksum_value": {
                    "type": "string"
                  },
                  "checksum_type": {
                    "type": "string"
                  }
                },
                "required": [
                  "bucket",
                  "key",
                  "payload",
                  "size",
                  "sha256",
                  "content_type"
                ],
                "additionalProperties": false
              }
            },
            "required": [
              "bucket",
              "key",
              "kind"
            ],
            "additionalProperties": false
          },
          "bucket_change": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "bucket": {
                "type": "string"
              },
              "kind": {
                "type": "string"
              }
            },
            "required": [
              "bucket",
              "kind"
            ],
            "additionalProperties": false
          },
          "kind": {
            "type": "string"
          },
          "file": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "path": {
                "type": "string"
              },
              "size": {
                "type": "integer"
              },
              "mode": {
                "type": "integer",
                "minimum": 0,
                "maximum": 4294967295
              },
              "sha256": {
                "type": "string"
              }
            },
            "required": [
              "path",
              "size",
              "mode",
              "sha256"
            ],
            "additionalProperties": false
          },
          "change": {
            "type": [
              "null",
              "object"
            ],
            "properties": {
              "path": {
                "type": "string"
              },
              "kind": {
                "type": "string"
              },
              "from": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "path": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "mode": {
                    "type": "integer",
                    "minimum": 0,
                    "maximum": 4294967295
                  },
                  "sha256": {
                    "type": "string"
                  }
                },
                "required": [
                  "path",
                  "size",
                  "mode",
                  "sha256"
                ],
                "additionalProperties": false
              },
              "to": {
                "type": [
                  "null",
                  "object"
                ],
                "properties": {
                  "path": {
                    "type": "string"
                  },
                  "size": {
                    "type": "integer"
                  },
                  "mode": {
                    "type": "integer",
                    "minimum": 0,
                    "maximum": 4294967295
                  },
                  "sha256": {
                    "type": "string"
                  }
                },
                "required": [
                  "path",
                  "size",
                  "mode",
                  "sha256"
                ],
                "additionalProperties": false
              }
            },
            "required": [
              "path",
              "kind"
            ],
            "additionalProperties": false
          },
          "excluded": {
            "type": "string"
          }
        },
        "required": [
          "kind"
        ],
        "additionalProperties": false
      }
    },
    "next_cursor": {
      "type": "string"
    },
    "handoff": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "version": {
          "type": "integer"
        },
        "workspace_id": {
          "type": "string"
        },
        "checkpoint_id": {
          "type": "string"
        },
        "registry_dir": {
          "type": "string"
        },
        "team": {
          "type": "string"
        },
        "archive": {
          "type": [
            "null",
            "object"
          ],
          "properties": {
            "path": {
              "type": "string"
            },
            "sha256": {
              "type": "string"
            },
            "bytes": {
              "type": "integer"
            },
            "files": {
              "type": "integer"
            }
          },
          "required": [
            "path",
            "sha256",
            "bytes",
            "files"
          ],
          "additionalProperties": false
        }
      },
      "required": [
        "version",
        "workspace_id"
      ],
      "additionalProperties": false
    },
    "bundle": {
      "type": "string"
    },
    "origin_checkpoint_id": {
      "type": "string"
    },
    "error": {
      "type": [
        "null",
        "object"
      ],
      "properties": {
        "code": {
          "type": "string"
        },
        "phase": {
          "type": "string"
        },
        "attempts": {
          "type": "integer"
        },
        "retryable": {
          "type": "boolean"
        },
        "message": {
          "type": "string"
        }
      },
      "required": [
        "code",
        "phase",
        "attempts",
        "retryable",
        "message"
      ],
      "additionalProperties": false
    }
  },
  "required": [
    "version",
    "outcome"
  ],
  "additionalProperties": false
}
```
