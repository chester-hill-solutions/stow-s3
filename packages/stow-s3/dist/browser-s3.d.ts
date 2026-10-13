import { type S3Client } from "@aws-sdk/client-s3";
import { type ObjectSyncRemote, type SyncRemoteObject, type SyncRemoteWrite } from "./file-history-types.js";
/** Configure the client's endpoint, scoped credentials and timeouts in the host application. */
export declare class S3ObjectSyncRemote implements ObjectSyncRemote {
    private readonly client;
    constructor(client: S3Client);
    headObject(bucket: string, key: string): Promise<{
        etag: string;
        size: number;
        metadata?: Record<string, string>;
        sha256?: string;
    } | null>;
    getObject(bucket: string, key: string): Promise<SyncRemoteObject | null>;
    putObject(bucket: string, key: string, data: Uint8Array, options: SyncRemoteWrite): Promise<{
        etag: string;
    }>;
}
//# sourceMappingURL=browser-s3.d.ts.map