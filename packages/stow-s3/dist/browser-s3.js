import { GetObjectCommand, HeadObjectCommand, PutObjectCommand } from "@aws-sdk/client-s3";
import { FileHistoryError, } from "./file-history-types.js";
const saveIdMetadata = "stow-save-id";
/** Configure the client's endpoint, scoped credentials and timeouts in the host application. */
export class S3ObjectSyncRemote {
    client;
    constructor(client) {
        this.client = client;
    }
    async headObject(bucket, key) {
        try {
            const result = await this.client.send(new HeadObjectCommand({ Bucket: bucket, Key: key, ChecksumMode: "ENABLED" }));
            if (!result.ETag || result.ContentLength === undefined)
                throw new FileHistoryError("remote_unavailable", "S3 did not return object identity and length");
            const sha256 = result.ChecksumSHA256 && result.ChecksumType === "FULL_OBJECT" ? checksumHex(result.ChecksumSHA256) : undefined;
            return { etag: result.ETag, size: result.ContentLength, metadata: result.Metadata, sha256 };
        }
        catch (error) {
            if (hasStatus(error, 404))
                return null;
            throw error;
        }
    }
    async getObject(bucket, key) {
        try {
            const result = await this.client.send(new GetObjectCommand({ Bucket: bucket, Key: key }));
            if (!result.Body || !result.ETag)
                throw new FileHistoryError("remote_unavailable", "S3 did not return object bytes and ETag");
            const metadata = { ...result.Metadata };
            const saveId = metadata[saveIdMetadata];
            delete metadata[saveIdMetadata];
            return {
                data: await result.Body.transformToByteArray(), etag: result.ETag,
                contentType: result.ContentType, metadata, saveId,
            };
        }
        catch (error) {
            if (hasStatus(error, 404))
                return null;
            throw error;
        }
    }
    async putObject(bucket, key, data, options) {
        if (Object.keys(options.metadata ?? {}).some((key) => key.toLowerCase() === saveIdMetadata)) {
            throw new FileHistoryError("invalid_options", `${saveIdMetadata} metadata is reserved for write reconciliation`);
        }
        try {
            const result = await this.client.send(new PutObjectCommand({
                Bucket: bucket, Key: key, Body: data,
                ContentType: options.contentType,
                Metadata: { ...options.metadata, [saveIdMetadata]: options.saveId },
                ...(options.sha256 ? { ChecksumAlgorithm: "SHA256", ChecksumSHA256: checksumBase64(options.sha256) } : {}),
                ...("etag" in options.basis ? { IfMatch: options.basis.etag } : { IfNoneMatch: "*" }),
            }));
            if (!result.ETag)
                throw new FileHistoryError("uncertain_write", "S3 write returned no ETag; retry sync to reconcile it");
            return { etag: result.ETag };
        }
        catch (error) {
            if (hasStatus(error, 412))
                throw new FileHistoryError("conflict", `remote object ${bucket}/${key} changed; local stages were retained`);
            // 409 is retryable and does not prove a failed precondition; retain the attempt.
            throw error;
        }
    }
}
function checksumBase64(hex) {
    if (!/^[a-f0-9]{64}$/.test(hex))
        throw new FileHistoryError("invalid_options", "invalid SHA-256 digest");
    return btoa(String.fromCharCode(...Uint8Array.from(hex.match(/../g) ?? [], (pair) => Number.parseInt(pair, 16))));
}
function checksumHex(base64) {
    const bytes = Uint8Array.from(atob(base64), (character) => character.charCodeAt(0));
    if (bytes.length !== 32)
        throw new FileHistoryError("remote_unavailable", "invalid provider SHA-256 checksum");
    return Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
}
function hasStatus(error, status) {
    if (typeof error !== "object" || error === null || !("$metadata" in error))
        return false;
    const metadata = error.$metadata;
    return typeof metadata === "object" && metadata !== null && "httpStatusCode" in metadata && metadata.httpStatusCode === status;
}
//# sourceMappingURL=browser-s3.js.map