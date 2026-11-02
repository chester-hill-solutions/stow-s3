// A stand-in for the AWS SDK's command classes.
//
// The recipe only constructs commands and hands them to a client, so a class
// that records the input is enough. The shared corpus pins the wire-level 412
// and its PreconditionFailed code, so the errors raised against these commands
// carry the shape the SDK produces for that response.

export class Command {
  constructor(input) {
    this.input = input;
  }
}

export class GetObjectCommand extends Command {}

export class PutObjectCommand extends Command {}

export class ListObjectsV2Command extends Command {}

export class DeleteObjectCommand extends Command {}
