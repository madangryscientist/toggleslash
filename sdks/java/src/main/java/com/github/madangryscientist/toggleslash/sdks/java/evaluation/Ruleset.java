package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.ContextList;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Flag;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Header;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Segment;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Target;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * A validated and indexed ruleset ({@code {flags, segments}}, as {@link
 * com.github.madangryscientist.toggleslash.sdks.java.json.Json#parse} hands it over), built once so
 * each evaluation skips that work. Building never fails: a bad flag becomes an error result when it
 * is evaluated.
 */
public final class Ruleset {
  /** A target that lists a key: its place in the targets and the variation it serves. */
  static final class TargetHit {
    final int order;
    final int variation;

    TargetHit(int order, int variation) {
      this.order = order;
      this.variation = variation;
    }
  }

  /** A segment that is valid, with its key lists indexed by kind. */
  static final class ReadySegment {
    final Segment segment;
    final Map<String, Set<String>> included;
    final Map<String, Set<String>> excluded;

    ReadySegment(Segment segment) {
      this.segment = segment;
      this.included = keysByKind(segment.included, segment.includedContexts);
      this.excluded = keysByKind(segment.excluded, segment.excludedContexts);
    }
  }

  /** A segment entry; {@code ready} is null when the segment is malformed. */
  static final class SegmentEntry {
    final ReadySegment ready;

    SegmentEntry(ReadySegment ready) {
      this.ready = ready;
    }
  }

  /** What a flag is: not valid at all, valid to its header, or valid. */
  enum Status {
    MALFORMED,
    BROKEN,
    READY
  }

  /** A flag entry. */
  static final class FlagEntry {
    final Status status;
    final Header header;
    final ErrorCode code;
    final Flag flag;
    final Map<String, Map<String, TargetHit>> targets;

    FlagEntry(
        Status status,
        Header header,
        ErrorCode code,
        Flag flag,
        Map<String, Map<String, TargetHit>> targets) {
      this.status = status;
      this.header = header;
      this.code = code;
      this.flag = flag;
      this.targets = targets;
    }
  }

  final Map<String, FlagEntry> flags;
  final Map<String, SegmentEntry> segments;

  private Ruleset(Map<String, FlagEntry> flags, Map<String, SegmentEntry> segments) {
    this.flags = flags;
    this.segments = segments;
  }

  private static Map<String, Set<String>> keysByKind(
      List<String> userKeys, List<ContextList> lists) {
    Map<String, Set<String>> index = new HashMap<>();
    index.put(Contexts.DEFAULT_KIND, new HashSet<>(userKeys));
    for (ContextList list : lists) {
      Set<String> keys = index.computeIfAbsent(list.contextKind, k -> new HashSet<>());
      keys.addAll(list.values);
    }
    return index;
  }

  private static Map<String, Map<String, TargetHit>> targetIndex(Flag flag) {
    Map<String, Map<String, TargetHit>> index = new HashMap<>();
    List<Target> targets = flag.targets;
    for (int order = 0; order < targets.size(); order++) {
      Target target = targets.get(order);
      String kind = target.contextKind == null ? Contexts.DEFAULT_KIND : target.contextKind;
      Map<String, TargetHit> keys = index.computeIfAbsent(kind, k -> new HashMap<>());
      for (String contextKey : target.contextKeys) {
        keys.putIfAbsent(contextKey, new TargetHit(order, target.variation));
      }
    }
    return index;
  }

  /**
   * The first reference to a segment that is missing or malformed, in rule, clause, value order.
   */
  private static ErrorCode segmentProblem(Flag flag, Map<String, SegmentEntry> segments) {
    for (Model.Rule rule : flag.rules) {
      for (Model.Clause clause : rule.clauses) {
        if (!(clause instanceof Model.SegmentClause)) {
          continue;
        }
        for (String key : ((Model.SegmentClause) clause).values) {
          SegmentEntry entry = segments.get(key);
          if (entry == null) {
            return ErrorCode.SEGMENT_NOT_FOUND;
          }
          if (entry.ready == null) {
            return ErrorCode.MALFORMED_SEGMENT;
          }
        }
      }
    }
    return null;
  }

  private static FlagEntry compileFlag(Object value, Map<String, SegmentEntry> segments) {
    Header header = Validate.header(value);
    if (header == null) {
      return new FlagEntry(Status.MALFORMED, null, null, null, null);
    }
    Flag flag = Validate.body(value, header);
    if (flag == null) {
      return new FlagEntry(Status.BROKEN, header, ErrorCode.MALFORMED_FLAG, null, null);
    }
    ErrorCode code = segmentProblem(flag, segments);
    if (code != null) {
      return new FlagEntry(Status.BROKEN, header, code, null, null);
    }
    return new FlagEntry(Status.READY, header, null, flag, targetIndex(flag));
  }

  /**
   * Validates and indexes a parsed ruleset. A value that is not an object, or a {@code flags} or
   * {@code segments} that is not an array, counts as empty. The first entry per key wins, even if
   * malformed; an entry with no string key is dropped.
   *
   * @param input the parsed JSON document
   * @return the ruleset
   */
  public static Ruleset create(Object input) {
    Object rawFlags = null;
    Object rawSegments = null;
    if (input instanceof Map) {
      rawFlags = ((Map<?, ?>) input).get("flags");
      rawSegments = ((Map<?, ?>) input).get("segments");
    }
    Map<String, SegmentEntry> segments = new HashMap<>();
    if (rawSegments instanceof List) {
      for (Object item : (List<?>) rawSegments) {
        Object key = item instanceof Map ? ((Map<?, ?>) item).get("key") : null;
        if (key instanceof String && !segments.containsKey(key)) {
          Segment segment = Validate.segment(item);
          segments.put(
              (String) key, new SegmentEntry(segment == null ? null : new ReadySegment(segment)));
        }
      }
    }
    Map<String, FlagEntry> flags = new HashMap<>();
    if (rawFlags instanceof List) {
      for (Object item : (List<?>) rawFlags) {
        Object key = item instanceof Map ? ((Map<?, ?>) item).get("key") : null;
        if (key instanceof String && !flags.containsKey(key)) {
          flags.put((String) key, compileFlag(item, segments));
        }
      }
    }
    return new Ruleset(flags, segments);
  }

  /**
   * The flag's {@code version}, or {@code null} when the ruleset has no such flag, the flag is
   * malformed (its own shape, or a segment it names), or the version does not fit a signed 64-bit
   * integer.
   *
   * @param flagKey the flag's key
   * @return the version or null
   */
  public Long flagVersion(String flagKey) {
    FlagEntry entry = flags.get(flagKey);
    if (entry == null || entry.status != Status.READY) {
      return null;
    }
    double version = entry.flag.version;
    return version < 9.223372036854775807E18 ? Long.valueOf((long) version) : null;
  }

  /**
   * How many flags the ruleset holds, malformed ones included.
   *
   * @return the count
   */
  public int flagCount() {
    return flags.size();
  }
}
