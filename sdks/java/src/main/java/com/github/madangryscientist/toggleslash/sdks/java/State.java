package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;
import java.time.Instant;

/**
 * Everything a reader needs, immutable, held in one reference: the ruleset and the ETag that names
 * it swap together, and {@code status} is one snapshot.
 */
record State(
    Ruleset ruleset, String etag, Instant lastSyncAt, StreamState stream, String lastError) {

  State withStream(StreamState next) {
    return new State(ruleset, etag, lastSyncAt, next, lastError);
  }

  State withError(String code) {
    return new State(ruleset, etag, lastSyncAt, stream, code);
  }

  State synced(Ruleset nextRuleset, String nextEtag, Instant now) {
    return new State(nextRuleset, nextEtag, now, stream, null);
  }

  State touched(Instant now) {
    return new State(ruleset, etag, now, stream, null);
  }

  State connected() {
    return new State(ruleset, etag, lastSyncAt, StreamState.CONNECTED, null);
  }

  boolean holdsEtag() {
    return ruleset != null && etag != null && !etag.isEmpty();
  }
}
