package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/** Why a flag answered as it did, spelled as in the contract. */
public enum ReasonKind {
  OFF,
  TARGET_MATCH,
  RULE_MATCH,
  FALLTHROUGH,
  PREREQUISITE_FAILED,
  ERROR;

  /** The canonical spelling, as it appears on the wire and in the specification. */
  public String value() {
    return name();
  }

  @Override
  public String toString() {
    return name();
  }
}
