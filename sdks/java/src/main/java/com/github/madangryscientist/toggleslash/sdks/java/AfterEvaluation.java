package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * Called on the caller's thread, synchronously, after every {@code variation} and {@code
 * variationDetail}, a fallback included. A failure it throws goes to {@code onError} and never
 * reaches the caller.
 */
@FunctionalInterface
public interface AfterEvaluation {
  /**
   * Observes one evaluation.
   *
   * @param event what the evaluation did
   * @throws Exception anything; it is passed to {@code onError}
   */
  void accept(EvaluationEvent event) throws Exception;
}
