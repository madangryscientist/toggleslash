package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/** Why a flag evaluated to what it did. */
public enum ReasonKind {
  OFF,
  PREREQUISITE_FAILED,
  TARGET_MATCH,
  RULE_MATCH,
  FALLTHROUGH,
  ERROR
}
