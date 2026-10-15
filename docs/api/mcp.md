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
