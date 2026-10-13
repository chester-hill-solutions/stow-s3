export class FileHistoryError extends Error {
    code;
    constructor(code, message) {
        super(message);
        this.code = code;
        this.name = "FileHistoryError";
    }
}
//# sourceMappingURL=file-history-types.js.map