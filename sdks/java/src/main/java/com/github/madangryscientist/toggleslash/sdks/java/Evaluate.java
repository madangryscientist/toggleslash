package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Result;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;

/** The evaluator, as a seam: a test passes one that raises. */
@FunctionalInterface
interface Evaluate {
  Result evaluate(Ruleset ruleset, String flagKey, Object context);
}
