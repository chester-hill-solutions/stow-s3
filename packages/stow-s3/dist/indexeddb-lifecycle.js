import { BrowserPersistenceError } from "./browser-types.js";
export function openIndexedDatabase(factory, options) {
    return new Promise((resolve, reject) => {
        let state = "pending";
        const request = factory.open(options.name, options.version);
        request.onupgradeneeded = () => {
            if (state !== "pending") {
                request.transaction?.abort();
                return;
            }
            options.upgrade(request.result);
        };
        request.onsuccess = () => {
            if (state !== "pending") {
                request.result.close();
                return;
            }
            state = "opened";
            request.result.onversionchange = () => request.result.close();
            resolve(request.result);
        };
        request.onerror = () => {
            state = "rejected";
            reject(request.error ?? new Error(options.failedMessage));
        };
        request.onblocked = () => {
            state = "rejected";
            reject(new BrowserPersistenceError("persistence_denied", options.blockedMessage));
        };
    });
}
//# sourceMappingURL=indexeddb-lifecycle.js.map