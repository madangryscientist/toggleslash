package com.github.madangryscientist.toggleslash.sdks.java;

/** Called after every evaluation, on the caller's thread. */
@FunctionalInterface
public interface Hook {
  /**
   * Observes one evaluation. A {@code Future} or {@code CompletionStage} it returns that later
   * fails is reported to the error callback.
   *
   * @param event the evaluation
   * @return anything; only a failing future or stage matters
   * @throws Exception any failure; it goes to the error callback
   */
  Object onEvaluation(EvaluationEvent event) throws Exception;
}
