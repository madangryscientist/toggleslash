package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;
import java.time.Instant;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicReference;
import java.util.function.Consumer;
import java.util.function.UnaryOperator;

/**
 * What the client, the sync loop and the usage flusher share: the options, the one state, the
 * closing flag and latch, the two Retry-After holds and the way an absorbed failure reaches {@code
 * onError}.
 */
final class Core {
  final Options options;
  final Pacer pacer;
  final AtomicReference<State> state;
  final AtomicBoolean closing = new AtomicBoolean();
  final CountDownLatch closed = new CountDownLatch(1);
  final Hold rulesetHold;
  final Hold usageHold;

  Core(Options options) {
    this.options = options;
    this.pacer = options.pacer();
    this.rulesetHold = new Hold(pacer);
    this.usageHold = new Hold(pacer);
    StreamState initial = options.stream() ? StreamState.CONNECTING : StreamState.OFF;
    this.state = new AtomicReference<>(new State(null, null, null, initial, null));
  }

  /**
   * The {@link LastError} code of a failed request.
   *
   * @param error what was thrown
   * @return {@code timeout} for a time-out, else {@code network}
   */
  static String codeOf(Throwable error) {
    boolean timeout =
        error instanceof java.util.concurrent.TimeoutException
            || error instanceof java.net.http.HttpTimeoutException;
    return timeout ? LastError.TIMEOUT : LastError.NETWORK;
  }

  Instant now() {
    return options.clock().instant();
  }

  /**
   * Applies a pure change to the state, unless the client is closed: a closed state never changes.
   */
  void apply(UnaryOperator<State> change) {
    state.updateAndGet(s -> s.stream() == StreamState.CLOSED ? s : change.apply(s));
  }

  void setStream(StreamState next) {
    apply(s -> s.withStream(next));
  }

  /** A sync with a new ruleset (a {@code 200}). */
  void synced(Ruleset ruleset, String etag) {
    Instant at = now();
    apply(s -> s.synced(ruleset, etag, at));
  }

  /** A sync that changed nothing (a {@code 304}, or a stream event naming the tag held). */
  void touched() {
    Instant at = now();
    apply(s -> s.touched(at));
  }

  void connected() {
    apply(State::connected);
  }

  /**
   * Records a failed ruleset or stream attempt and tells {@code onError}, unless {@code close}
   * caused it.
   *
   * @param code the {@link LastError} code
   * @param what what was being done
   */
  void fail(String code, String what) {
    if (closing.get()) {
      return;
    }
    apply(s -> s.withError(code));
    report(new ToggleSlashException(what + " failed: " + code, code));
  }

  /**
   * Passes an absorbed failure to {@code onError}. It never throws and holds no lock.
   *
   * @param error the failure
   */
  void report(Throwable error) {
    Consumer<Throwable> handler = options.onError();
    if (handler == null) {
      return;
    }
    try {
      handler.accept(error);
    } catch (Throwable ignored) {
      // An onError that throws is ignored (section 10).
    }
  }
}
