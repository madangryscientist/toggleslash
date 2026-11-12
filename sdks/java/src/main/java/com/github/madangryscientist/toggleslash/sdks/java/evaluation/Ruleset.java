package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * A ruleset validated and indexed once, so each evaluation skips that work. Building one never
 * throws: a bad flag becomes an {@code ERROR} result when it is evaluated.
 */
public final class Ruleset {
  /** A target that lists a key: its place in {@code targets} and the variation it serves. */
  record TargetHit(int order, int variation) {}

  /** How far a flag got through validation. */
  enum Status {
    MALFORMED,
    BROKEN,
    READY
  }

  /** One flag. A malformed one has neither header nor body; a broken one has a header only. */
  static final class FlagEntry {
    final Status status;
    final Object flag;
    final ErrorCode code;
    final Map<String, Map<String, TargetHit>> targets;

    FlagEntry(Status status, Object flag, ErrorCode code, Map<String, Map<String, TargetHit>> t) {
      this.status = status;
      this.flag = flag;
      this.code = code;
      this.targets = t;
    }
  }

  /** One segment, or a marker for a malformed one. */
  static final class SegmentEntry {
    final boolean malformed;
    final Object segment;
    final Map<String, Set<String>> included;
    final Map<String, Set<String>> excluded;

    SegmentEntry(
        boolean malformed,
        Object segment,
        Map<String, Set<String>> included,
        Map<String, Set<String>> excluded) {
      this.malformed = malformed;
      this.segment = segment;
      this.included = included;
      this.excluded = excluded;
    }
  }

  private final Map<String, FlagEntry> flags;
  private final Map<String, SegmentEntry> segments;

  private Ruleset(Map<String, FlagEntry> flags, Map<String, SegmentEntry> segments) {
    this.flags = flags;
    this.segments = segments;
  }

  /**
   * Validates and indexes a ruleset.
   *
   * @param input the parsed JSON of a ruleset, an object with {@code flags} and {@code segments};
   *     anything else is an empty ruleset
   * @return the ruleset
   */
  public static Ruleset create(Object input) {
    Object rawFlags = Js.isRecord(input) ? Js.member(input, "flags") : null;
    Object rawSegments = Js.isRecord(input) ? Js.member(input, "segments") : null;
    Map<String, SegmentEntry> segments = new HashMap<>();
    for (Object item : items(rawSegments)) {
      Object key = Js.isRecord(item) ? Js.member(item, "key") : null;
      if (key instanceof String && !segments.containsKey(key)) {
        segments.put((String) key, compileSegment(item));
      }
    }
    Map<String, FlagEntry> flags = new HashMap<>();
    for (Object item : items(rawFlags)) {
      Object key = Js.isRecord(item) ? Js.member(item, "key") : null;
      if (key instanceof String && !flags.containsKey(key)) {
        flags.put((String) key, compileFlag(item, segments));
      }
    }
    return new Ruleset(flags, segments);
  }

  private static List<?> items(Object value) {
    return Js.isArray(value) ? Js.array(value) : List.of();
  }

  private static Map<String, Set<String>> keysByKind(Object userKeys, Object lists) {
    Map<String, Set<String>> index = new HashMap<>();
    Set<String> users = new HashSet<>();
    for (Object key : Js.array(userKeys)) {
      users.add((String) key);
    }
    index.put(Contexts.DEFAULT_KIND, users);
    if (lists != null) {
      for (Object list : Js.array(lists)) {
        String kind = (String) Js.member(list, "contextKind");
        Set<String> keys = index.computeIfAbsent(kind, k -> new HashSet<>());
        for (Object value : Js.array(Js.member(list, "values"))) {
          keys.add((String) value);
        }
      }
    }
    return index;
  }

  private static SegmentEntry compileSegment(Object value) {
    if (!Validate.isSegment(value)) {
      return new SegmentEntry(true, null, null, null);
    }
    return new SegmentEntry(
        false,
        value,
        keysByKind(Js.member(value, "included"), Js.member(value, "includedContexts")),
        keysByKind(Js.member(value, "excluded"), Js.member(value, "excludedContexts")));
  }

  /**
   * The first reference to a segment that is missing or malformed, in rule, clause, value order.
   */
  private static ErrorCode segmentProblem(Object flag, Map<String, SegmentEntry> segments) {
    for (Object rule : Js.array(Js.member(flag, "rules"))) {
      for (Object clause : Js.array(Js.member(rule, "clauses"))) {
        if (!"segmentMatch".equals(Js.member(clause, "op"))) {
          continue;
        }
        for (Object key : Js.array(Js.member(clause, "values"))) {
          SegmentEntry segment = segments.get((String) key);
          if (segment == null) {
            return ErrorCode.SEGMENT_NOT_FOUND;
          }
          if (segment.malformed) {
            return ErrorCode.MALFORMED_SEGMENT;
          }
        }
      }
    }
    return null;
  }

  private static Map<String, Map<String, TargetHit>> targetIndex(Object flag) {
    Map<String, Map<String, TargetHit>> index = new HashMap<>();
    int order = 0;
    for (Object target : Js.array(Js.member(flag, "targets"))) {
      Object declared = Js.member(target, "contextKind");
      String kind = declared == null ? Contexts.DEFAULT_KIND : (String) declared;
      Map<String, TargetHit> keys = index.computeIfAbsent(kind, k -> new HashMap<>());
      int variation = (int) Js.number(Js.member(target, "variation"));
      for (Object contextKey : Js.array(Js.member(target, "contextKeys"))) {
        keys.putIfAbsent((String) contextKey, new TargetHit(order, variation));
      }
      order++;
    }
    return index;
  }

  private static FlagEntry compileFlag(Object value, Map<String, SegmentEntry> segments) {
    if (!Validate.isFlagHeader(value)) {
      return new FlagEntry(Status.MALFORMED, null, null, null);
    }
    int variationCount = Js.array(Js.member(value, "variations")).size();
    if (!Validate.isFlagBody(value, variationCount)) {
      return new FlagEntry(Status.BROKEN, value, ErrorCode.MALFORMED_FLAG, null);
    }
    ErrorCode code = segmentProblem(value, segments);
    if (code != null) {
      return new FlagEntry(Status.BROKEN, value, code, null);
    }
    return new FlagEntry(Status.READY, value, null, targetIndex(value));
  }

  FlagEntry flag(String key) {
    return flags.get(key);
  }

  SegmentEntry segment(String key) {
    return segments.get(key);
  }

  /** Tells whether the context has, in any of its kinds, a key that {@code keys} lists there. */
  static boolean listsContext(
      Map<String, Set<String>> keys, Map<String, Contexts.KindContext> contexts) {
    for (Map.Entry<String, Contexts.KindContext> entry : contexts.entrySet()) {
      Set<String> set = keys.get(entry.getKey());
      if (set != null && set.contains(entry.getValue().key())) {
        return true;
      }
    }
    return false;
  }

  /** The variation of the first target in array order that matches the context, or -1. */
  static int targetedVariation(
      Map<String, Map<String, TargetHit>> targets, Map<String, Contexts.KindContext> contexts) {
    TargetHit first = null;
    for (Map.Entry<String, Contexts.KindContext> entry : contexts.entrySet()) {
      Map<String, TargetHit> keys = targets.get(entry.getKey());
      TargetHit hit = keys == null ? null : keys.get(entry.getValue().key());
      if (hit != null && (first == null || hit.order() < first.order())) {
        first = hit;
      }
    }
    return first == null ? -1 : first.variation();
  }

  /**
   * The {@code version} of a flag that is valid and whose segments are all present and valid.
   *
   * @param key the flag key
   * @return the version, or null when the flag is absent, malformed, or names a segment that is
   *     missing or malformed, or when the version does not fit a signed 64-bit integer
   */
  public Long flagVersion(String key) {
    FlagEntry entry = flags.get(key);
    if (entry == null || entry.status != Status.READY) {
      return null;
    }
    double version = Js.number(Js.member(entry.flag, "version"));
    return version < 9.223372036854775807E18 ? Long.valueOf((long) version) : null;
  }

  /**
   * How many flags the ruleset holds.
   *
   * @return the count of distinct flag keys
   */
  public int flagCount() {
    return flags.size();
  }
}
