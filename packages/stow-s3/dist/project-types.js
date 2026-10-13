export class ProjectError extends Error {
    code;
    constructor(code, message) {
        super(message);
        this.code = code;
        this.name = "ProjectError";
    }
}
//# sourceMappingURL=project-types.js.map