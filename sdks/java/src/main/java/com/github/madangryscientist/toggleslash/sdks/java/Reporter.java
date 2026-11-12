package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.function.Consumer;

/** Calls {@code onError}; what it raises is ignored. */
final class Reporter {
  private final Consumer<Throwable> onError;

  Reporter(Consumer<Throwable> onError) {
    this.onError = onError;
  }

  void report(Throwable error) {
    if (onError == null) {
      return;
    }
    try {
      onError.accept(error);
    } catch (Throwable ignored) {
      // The contract: an onError that raises is ignored.
    }
  }

  void report(String message) {
    report(new ToggleSlashException(message));
  }
}
