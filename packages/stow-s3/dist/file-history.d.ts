import type { BrowserEmbeddedStow } from "./persistent-embedded.js";
import { type BrowserFileHistoryOptions, type FileHistoryStatus, type FileReadResult, type FileSaveOptions, type FileSaveReceipt, type FileHistoryExport, type FileVersion } from "./file-history-types.js";
/** Durable file stages over the browser store. Networking happens only on an explicit read miss or sync. */
export declare class BrowserFileHistory {
    private readonly storage;
    private readonly remote;
    private readonly queue;
    private constructor();
    static open(store: BrowserEmbeddedStow, options: BrowserFileHistoryOptions): Promise<BrowserFileHistory>;
    read(key: string): Promise<FileReadResult>;
    save(key: string, data: Uint8Array, options?: FileSaveOptions): Promise<FileReadResult>;
    saveReceipt(key: string, data: Uint8Array, options?: FileSaveOptions): Promise<FileSaveReceipt>;
    private append;
    status(key: string): Promise<FileHistoryStatus>;
    /** Holds the source history stable while copying one retained revision at a time. */
    exportRetained(key: string, visit: (version: FileVersion, bytes: Uint8Array, history: FileHistoryExport) => Promise<void>): Promise<FileHistoryExport>;
    undo(key: string): Promise<FileReadResult>;
    redo(key: string): Promise<FileReadResult>;
    /** Publish only the current stage; older stages stay local. */
    sync(key: string): Promise<FileHistoryStatus>;
    /** Explicitly choose the remote file after a conflict; retained local stages remain undoable. */
    adoptRemote(key: string): Promise<FileReadResult>;
    private move;
    private result;
    private requireRecord;
    private getRemote;
    private requireRemote;
    private enqueue;
}
//# sourceMappingURL=file-history.d.ts.map