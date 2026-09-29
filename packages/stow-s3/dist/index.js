import { buildAwsSdkV3Config, createStowConnection } from "./instance.js";
import { startStow } from "./start.js";
import { upstreamFromEnv } from "./upstream.js";
export { EMBEDDED_PROTOCOL_VERSION, EmbeddedStow, EmbeddedStowError, } from "./embedded.js";
// The browser persistence profile is deliberately not re-exported here. It is
// reachable only through the "./browser" subpath so that browser-only code
// never enters the Node entry point, and so the profile has exactly one
// documented import path.
export { loadNodeWasmHost, } from "./node-wasm-host.js";
// The ownership guard is public so a caller that manages its own data
// directories can check one without starting a server.
export { resetOwnedData, STOW_OWNER_MARKER, StowOwnershipError, } from "./ownership.js";
export { parseReadyLine } from "./start.js";
export { DEFAULT_SESSION_MAX_BYTES, DEFAULT_SESSION_MAX_OBJECTS, openStow, withStow, } from "./session.js";
export { READY_PROTOCOL_VERSION, StowProtocolError, parseReadyMessage, } from "./ready.js";
export { StowBinaryNotFoundError, resolveStowBinary, stowBinaryAvailable, } from "./bin.js";
export { upstreamFromEnv } from "./upstream.js";
export { checkpointWorkspace, collectWorkspaces, pruneWorkspaces, destroyWorkspace, diffWorkspaces, exportWorkspaceCheckpoint, handoffWorkspace, importWorkspaceCheckpoint, prepareWorkspace, restoreWorkspaceCheckpoint, resumeWorkspace, runWorkspaceCommand, } from "./workspace.js";
export const Stow = {
    start(options) {
        return startStow(options);
    },
    connect(options) {
        return createStowConnection(options);
    },
    awsSdkV3Config(options) {
        if ("stop" in options) {
            return buildAwsSdkV3Config({
                endpoint: options.endpoint,
                accessKeyId: options.accessKeyId,
                secretAccessKey: options.secretAccessKey,
                region: options.region,
                forcePathStyle: true,
            });
        }
        return buildAwsSdkV3Config({
            ...options,
            // No override. The caller's forcePathStyle is part of the public type and
            // buildAwsSdkV3Config already defaults it to true, so spreading the options
            // and then replacing the field with `true` made a declared option
            // impossible to set: a caller asking for virtual-hosted addressing got
            // path-style and a 404 from a provider that never sees the request.
            //
            // The StowInstance arm above keeps a fixed true. An instance is stow's own
            // loopback server with a generated bucket, where path-style is the address
            // that works, and StowInstance carries no addressing field to forward.
        });
    },
    upstream: {
        fromEnv() {
            return upstreamFromEnv();
        },
    },
};
export default Stow;
export { serveWorkspace } from "./workspace-serve.js";
//# sourceMappingURL=index.js.map