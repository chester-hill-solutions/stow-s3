import { GetObjectCommand, PutObjectCommand } from "@aws-sdk/client-s3";
import { getSignedUrl } from "@aws-sdk/s3-request-presigner";

export async function signBrowserTestObject(fixture, request) {
  const chunks = [];
  let size = 0;
  for await (const chunk of request) {
    const bytes = Buffer.from(chunk); size += bytes.length;
    if (size > 32_768) throw new Error("Scoped signing request is too large");
    chunks.push(bytes);
  }
  const body = JSON.parse(Buffer.concat(chunks).toString("utf8"));
  if (typeof body.key !== "string" || !body.key || body.key.split("/").some((part) => part === "." || part === "..")) throw new Error("Invalid scoped test key");
  const input = { Bucket: fixture.bucket, Key: `${fixture.prefix}${body.key}` };
  const headers = {};
  let command;
  if (body.method === "GET") command = new GetObjectCommand(input);
  else if (body.method === "PUT") {
    const { basis, saveId, contentType, metadata = {} } = body.options;
    if (!saveId || (!basis.etag && basis.absent !== true)) throw new Error("Conditional test write required");
    headers[basis.etag ? "if-match" : "if-none-match"] = basis.etag ?? "*";
    headers["content-type"] = contentType ?? "application/octet-stream";
    headers["x-amz-meta-stow-save-id"] = saveId;
    for (const [key, value] of Object.entries(metadata)) headers[`x-amz-meta-${key.toLowerCase()}`] = value;
    command = new PutObjectCommand({
      ...input, ContentType: headers["content-type"], Metadata: { ...metadata, "stow-save-id": saveId },
      ...(basis.etag ? { IfMatch: basis.etag } : { IfNoneMatch: "*" }),
    });
  } else throw new Error("Unsupported signed test method");
  const url = await getSignedUrl(fixture.signingClient, command, {
    expiresIn: 60,
    signableHeaders: new Set(Object.keys(headers)),
    unhoistableHeaders: new Set(Object.keys(headers).filter((key) => key.startsWith("x-amz-"))),
  });
  return { url, headers };
}
