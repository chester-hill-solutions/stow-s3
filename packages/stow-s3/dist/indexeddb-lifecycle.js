import { BrowserPersistenceError } from "./browser-types.js";
export function openIndexedDatabase(factory, options) {
    return new Promise((resolve, reject) => {
        const request = factory.open(options.name, options.version);
        request.onupgradeneeded = () => options.upgrade(request.result);
        request.onsuccess = () => {
            request.result.onversionchange = () => request.result.close();
            resolve(request.result);
        };
        request.onerror = () => reject(request.error ?? new Error(options.failedMessage));
        request.onblocked = () => reject(new BrowserPersistenceError("persistence_denied", options.blockedMessage));
    });
}
//# sourceMappingURL=indexeddb-lifecycle.js.map