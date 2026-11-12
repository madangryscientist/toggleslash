package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;

/** A ruleset and the ETag that names it, swapped together so a reader never sees a mix. */
record Snapshot(Ruleset ruleset, String etag) {}
