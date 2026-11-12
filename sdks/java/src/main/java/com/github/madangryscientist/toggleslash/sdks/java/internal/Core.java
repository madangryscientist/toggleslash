package com.github.madangryscientist.toggleslash.sdks.java.internal;

import com.github.madangryscientist.toggleslash.sdks.java.StreamState;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Ruleset;

/**
 * Everything a reader needs, in one immutable value, so one read is one consistent snapshot.
 *
 * @param ruleset the compiled ruleset, or null before the first sync
 * @param etag the ETag that came with it, or null
 * @param lastSyncAt epoch milliseconds of the last sync, or null
 * @param stream the stream state
 * @param lastError the last error code, or null
 */
public record Core(
    Ruleset ruleset, String etag, Long lastSyncAt, StreamState stream, String lastError) {
  Core withStream(StreamState value) {
    return new Core(ruleset, etag, lastSyncAt, value, lastError);
  }

  Core withError(String value) {
    return new Core(ruleset, etag, lastSyncAt, stream, value);
  }

  Core synced(long now) {
    return new Core(ruleset, etag, now, stream, null);
  }

  Core synced(Ruleset newRuleset, String newEtag, long now) {
    return new Core(newRuleset, newEtag, now, stream, null);
  }
}
