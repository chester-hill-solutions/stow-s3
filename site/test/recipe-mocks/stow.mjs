// A stand-in for the Stow client package, used to run a recipe against a
// scripted service.
//
// The recipe body is not changed to make this work. The test redirects the two
// modules the recipe imports to files like these, and the recipe runs exactly
// as it is published.
//
// The scenario is chosen with STOW_RECIPE_SCENARIO. Each one is a service that
// misbehaves in exactly one way, so a check can show that the recipe notices.

const SCENARIOS = {
  // The service refuses the stale write for the right reason and keeps the
  // version that was saved. The recipe passes.
  "stale-refused": {
    refusal: { name: "PreconditionFailed", code: "PreconditionFailed", status: 412 },
    retained: '{"version":2}',
  },
  // The service refuses the request for an unrelated reason. The recipe has
  // proved nothing about the write it was meant to make.
  "access-denied": {
    refusal: { name: "AccessDenied", code: "AccessDenied", status: 403 },
    retained: '{"version":2}',
  },
  // The service refuses the stale write correctly but stores something else.
  // A refusal is only worth anything if the object it refused to replace is
  // intact.
  "wrong-retained": {
    refusal: { name: "PreconditionFailed", code: "PreconditionFailed", status: 412 },
    retained: '{"version":999}',
  },
};

function scenario() {
  const name = process.env.STOW_RECIPE_SCENARIO ?? "stale-refused";
  const chosen = SCENARIOS[name];
  if (!chosen) {
    throw new Error(`unknown recipe scenario: ${name}`);
  }
  return chosen;
}

class S3Error extends Error {
  constructor(refusal) {
    super(`${refusal.code}: the request was refused`);
    this.name = refusal.name;
    this.$metadata = { httpStatusCode: refusal.status };
  }
}

function client() {
  const chosen = scenario();
  let conditionalWrites = 0;
  return {
    async send(command) {
      const input = command.input ?? {};
      if (input.Body === undefined) {
        return { Body: { transformToString: async () => chosen.retained } };
      }
      if (input.IfMatch === undefined) {
        return { ETag: '"etag-1"' };
      }
      conditionalWrites += 1;
      if (conditionalWrites === 1) {
        return { ETag: '"etag-2"' };
      }
      throw new S3Error(chosen.refusal);
    },
  };
}

export async function withStow(callback) {
  return callback({ s3: client(), bucket: "probe" });
}

export async function openStow() {
  return { s3: client(), close: async () => {} };
}
