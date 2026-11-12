package com.github.madangryscientist.toggleslash.sdks.java;

/** A hook the client calls after every evaluation, on the caller's thread. */
@FunctionalInterface
public interface AfterEvaluation {
  /**
   * Called once per {@code variation} or {@code variationDetail} call, before it returns. What it
   * throws goes to {@code onError} and changes nothing for the caller.
   *
   * @param event the evaluation
   */
  void accept(EvaluationEvent event);
}
