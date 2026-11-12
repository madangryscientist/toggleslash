package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.function.Consumer;

/** Hands a failure to the caller's {@code onError}, and ignores whatever that does. */
final class Reporter {
  private final Consumer<Throwable> onError;

  Reporter(Consumer<Throwable> onError) {
    this.onError = onError;
  }

  void report(Throwable failure) {
    if (onError == null) {
      return;
    }
    try {
      onError.accept(failure);
    } catch (Throwable ignored) {
      // A throw from onError is ignored: there is nowhere left to send it.
    }
  }
}
