# Node WASM host API

Load the packaged Go WASM runtime in Node. Import from `@chester-hill-solutions/stow-s3/node-wasm`.

## Defaults and limits

| Setting | Value |
| --- | --- |
| Assets | Packaged stow.wasm and wasm_exec.js |
| loadNodeWasmHost | Asynchronous initialization |
| call / callBinary | Synchronous object dispatch |
| callBinaryAsync / openHost | Asynchronous host-backed dispatch |

## Behavior

- Pass the returned host to EmbeddedStow.open or an asynchronous host-backed runtime.
- Close runtime handles before awaiting host.close.
- Host-backed stores must use asynchronous dispatch because adapter operations can await promises.

## Errors

- Loading fails if the runtime assets cannot be read or WASM initialization fails.
- Dispatch can return structured embedded errors.
- Synchronous dispatch rejects host-backed handles.

## Example

```ts
import { loadNodeWasmHost } from "@chester-hill-solutions/stow-s3/node-wasm";
const host = await loadNodeWasmHost();
try {
  // Pass host to EmbeddedStow.open or your host-backed adapter.
} finally {
  await host.close();
}
```

## Symbols

[loadNodeWasmHost](#loadnodewasmhost) · [NodeWasmHost](#nodewasmhost)

## loadNodeWasmHost



```ts
loadNodeWasmHost(): Promise<NodeWasmHost>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |

Returns: `Promise<NodeWasmHost>`.

## NodeWasmHost



```ts
interface NodeWasmHost  extends EmbeddedHost {
  callBinaryAsync(metadata: string, data?: Uint8Array<ArrayBufferLike> | undefined): Promise<EmbeddedBinaryResponse>;
  openHost(metadata: string, store: HostObjectStoreBridge): Promise<EmbeddedBinaryResponse>;
  close(): Promise<void>;
}
```

### NodeWasmHost.callBinaryAsync



```ts
callBinaryAsync(metadata: string, data?: Uint8Array<ArrayBufferLike> | undefined): Promise<EmbeddedBinaryResponse>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `metadata` | `string` | Yes | — | Serialized binary-bridge request or response metadata. |
| `data` | `Uint8Array<ArrayBufferLike> \| undefined` | No | — | Object bytes. Input ownership and size rules depend on the method. |

Returns: `Promise<EmbeddedBinaryResponse>`.

### NodeWasmHost.openHost



```ts
openHost(metadata: string, store: HostObjectStoreBridge): Promise<EmbeddedBinaryResponse>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `metadata` | `string` | Yes | — | Serialized binary-bridge request or response metadata. |
| `store` | `HostObjectStoreBridge` | Yes | — | Open underlying object or content store supplied by the application. |

Returns: `Promise<EmbeddedBinaryResponse>`.

### NodeWasmHost.close

Release this handle. Do not submit new operations after closing it.

```ts
close(): Promise<void>
```

Returns: `Promise<void>`.
