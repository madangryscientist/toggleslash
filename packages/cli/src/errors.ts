/** Exit codes, a contract (docs in README): 0 ok, 1 the server or the network failed, 2 a usage error. */
export const EXIT_OK = 0;
export const EXIT_FAILURE = 1;
export const EXIT_USAGE = 2;

/** A failure the CLI reports as one line on stderr and an exit code. */
export class CliError extends Error {
  readonly exitCode: number;
  constructor(message: string, exitCode: number) {
    super(message);
    this.name = "CliError";
    this.exitCode = exitCode;
  }
}

/** Bad arguments, a missing option or a missing setting: the caller can fix it by changing the command. Exit 2. */
export class UsageError extends CliError {
  constructor(message: string) {
    super(message, EXIT_USAGE);
    this.name = "UsageError";
  }
}

/** The server answered with an error, or could not be reached, or sent something unreadable. Exit 1. */
export class FailureError extends CliError {
  constructor(message: string) {
    super(message, EXIT_FAILURE);
    this.name = "FailureError";
  }
}
