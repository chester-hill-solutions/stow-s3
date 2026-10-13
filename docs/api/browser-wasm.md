# Browser WASM loader API

Load WASM in a worker or in the current JavaScript realm. Import from `@chester-hill-solutions/stow-s3/browser-wasm`.

## Defaults and limits

| Setting | Value |
| --- | --- |
| wasmUrl / runtimeUrl | Packaged assets relative to the module |
| workerUrl | Packaged module worker |
| Memory loader | loadBrowserWasmHost |
| Persistent loader | loadBrowserPersistentWasmHost |

## Behavior

- The worker loaders return a host immediately; calls wait for initialization.
- loadBrowserWasmBridge resolves after initialization in the current realm.
- A custom workerUrl must implement the embedded worker protocol.
- Close runtimes before host.close; closing the browser worker host terminates its worker.

## Errors

- Fetch, compilation or worker startup failures reject initialization or pending calls.
- Configure correct asset URLs when bundling.
- Host admission and cancellation rules are the same as the worker API.

## Example

```ts
import { loadBrowserWasmHost } from "@chester-hill-solutions/stow-s3/browser-wasm";
import { AsyncEmbeddedStow } from "@chester-hill-solutions/stow-s3/worker";
const host = loadBrowserWasmHost();
const store = await AsyncEmbeddedStow.open(host);
await store.close();
host.close();
```

## Symbols

[BrowserWasmBridge](#browserwasmbridge) · [BrowserWasmOptions](#browserwasmoptions) · [BrowserWorkerEmbeddedHost](#browserworkerembeddedhost) · [BrowserWorkerHostOptions](#browserworkerhostoptions) · [loadBrowserPersistentWasmHost](#loadbrowserpersistentwasmhost) · [loadBrowserWasmBridge](#loadbrowserwasmbridge) · [loadBrowserWasmHost](#loadbrowserwasmhost)

## BrowserWasmBridge



```ts
interface BrowserWasmBridge  extends AsyncWasmBridge {
  close(): Promise<void>;
}
```

### BrowserWasmBridge.close

Release this handle. Do not submit new operations after closing it.

```ts
close(): Promise<void>
```

Returns: `Promise<void>`.

## BrowserWasmOptions



```ts
interface BrowserWasmOptions {
  wasmUrl?: string | URL | undefined;
  runtimeUrl?: string | URL | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `wasmUrl` | `string \| URL \| undefined` | Yes | URL of the Go WASM asset; defaults to the packaged module-relative asset. |
| `runtimeUrl` | `string \| URL \| undefined` | Yes | URL of wasm_exec.js; defaults to the packaged module-relative asset. |

## BrowserWorkerEmbeddedHost



```ts
class BrowserWorkerEmbeddedHost  extends WorkerEmbeddedHost {
  constructor(worker: Worker, options?: WorkerHostOptions);
  close(): void;
}
```

### BrowserWorkerEmbeddedHost.constructor

function Object() { [native code] }

```ts
constructor(worker: Worker, options?: WorkerHostOptions)
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `worker` | `Worker` | Yes | — |  |
| `options` | `WorkerHostOptions` | No | {} |  |

Returns: `BrowserWorkerEmbeddedHost`.

### BrowserWorkerEmbeddedHost.close

Release this handle. Do not submit new operations after closing it.

```ts
close(): void
```

Returns: `void`.

## BrowserWorkerHostOptions



```ts
interface BrowserWorkerHostOptions  extends WorkerHostOptions {
  workerUrl?: string | URL | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `workerUrl` | `string \| URL \| undefined` | Yes | Optional custom module-worker URL implementing the embedded worker protocol. |
| `maxInFlight` | `number \| undefined` | Yes | Maximum admitted worker calls; default is 4. |
| `maxInFlightBytes` | `number \| undefined` | Yes | Maximum worker request metadata plus payload bytes; default is 64 MiB. |
| `maxResponseBytes` | `number \| undefined` | Yes | Maximum worker response metadata plus payload bytes; default is 64 MiB. |

## loadBrowserPersistentWasmHost



```ts
loadBrowserPersistentWasmHost(options?: BrowserWorkerHostOptions): BrowserWorkerEmbeddedHost
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `BrowserWorkerHostOptions` | No | {} |  |

Returns: `BrowserWorkerEmbeddedHost`.

## loadBrowserWasmBridge

Loads Go in the current realm; use within a custom worker to supply host storage.

```ts
loadBrowserWasmBridge(options?: BrowserWasmOptions): Promise<BrowserWasmBridge>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `BrowserWasmOptions` | No | {} |  |

Returns: `Promise<BrowserWasmBridge>`.

## loadBrowserWasmHost



```ts
loadBrowserWasmHost(options?: BrowserWorkerHostOptions): BrowserWorkerEmbeddedHost
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `BrowserWorkerHostOptions` | No | {} |  |

Returns: `BrowserWorkerEmbeddedHost`.
