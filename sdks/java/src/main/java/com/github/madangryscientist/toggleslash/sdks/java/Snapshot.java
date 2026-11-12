package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;

/**
 * Everything the loader knows, as one immutable value. It lives in one atomic reference, so the
 * ruleset and the ETag that names it swap together and {@code status} reads one moment.
 */
record Snapshot(
    Ruleset ruleset, String etag, Long lastSyncAt, StreamState stream, String lastError) {
  Snapshot withStream(StreamState value) {
    return new Snapshot(ruleset, etag, lastSyncAt, value, lastError);
  }

  Snapshot withError(String code) {
    return new Snapshot(ruleset, etag, lastSyncAt, stream, code);
  }

  /** The held ruleset is confirmed current at {@code now}. */
  Snapshot synced(long now) {
    return new Snapshot(ruleset, etag, now, stream, null);
  }

  /** A new ruleset and its ETag, confirmed current at {@code now}. */
  Snapshot loaded(Ruleset value, String tag, long now) {
    return new Snapshot(value, tag, now, stream, null);
  }

  Snapshot withConnected() {
    return new Snapshot(ruleset, etag, lastSyncAt, StreamState.CONNECTED, null);
  }
}
