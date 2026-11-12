package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;
import java.util.concurrent.atomic.AtomicReference;
import java.util.function.Consumer;
import java.util.function.UnaryOperator;

/**
 * The one place the client's shared state lives. A {@link State} is immutable and replaced whole,
 * so the ruleset and its ETag always change together and a reader sees one or the other.
 */
final class Store {
  /** Everything {@code status} and an evaluation read. */
  record State(
      Ruleset ruleset, String etag, Long lastSyncMs, StreamState stream, String lastError) {
    State withStream(StreamState value) {
      return new State(ruleset, etag, lastSyncMs, value, lastError);
    }

    State withLastError(String value) {
      return new State(ruleset, etag, lastSyncMs, stream, value);
    }

    /** The ruleset is confirmed current: the sync time moves and the error clears. */
    State synced(long now) {
      return new State(ruleset, etag, now, stream, null);
    }

    State loaded(Ruleset newRuleset, String newEtag, long now) {
      return new State(newRuleset, newEtag, now, stream, null);
    }
  }

  private final AtomicReference<State> ref;
  private final Consumer<Throwable> onError;
  private volatile boolean closing;

  Store(boolean stream, Consumer<Throwable> onError) {
    this.ref =
        new AtomicReference<>(
            new State(null, null, null, stream ? StreamState.CONNECTING : StreamState.OFF, null));
    this.onError = onError;
  }

  State get() {
    return ref.get();
  }

  /** Applies a pure change; once the client is closed nothing changes. */
  void update(UnaryOperator<State> change) {
    ref.updateAndGet(s -> s.stream() == StreamState.CLOSED ? s : change.apply(s));
  }

  boolean closing() {
    return closing;
  }

  void close() {
    closing = true;
    ref.updateAndGet(s -> s.withStream(StreamState.CLOSED));
  }

  /** Tells {@code onError}; what it throws is ignored. */
  void report(Throwable error) {
    if (onError == null) {
      return;
    }
    try {
      onError.accept(error);
    } catch (Throwable ignored) {
      // A faulty error callback must not break the SDK.
    }
  }
}
