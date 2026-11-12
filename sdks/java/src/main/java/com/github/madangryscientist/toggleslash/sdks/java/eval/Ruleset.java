package com.github.madangryscientist.toggleslash.sdks.java.eval;

import com.github.madangryscientist.toggleslash.sdks.java.ErrorCode;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.Entry;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.Flag;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.Header;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.SegmentEntry;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.Target;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.TargetHit;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.function.Function;

/**
 * A ruleset (the JSON {@code {flags, segments}}) validated and indexed once, so each evaluation
 * skips that work. It is immutable.
 */
public final class Ruleset {
  private final Map<String, Entry> flags;
  private final Map<String, SegmentEntry> segments;

  private Ruleset(Map<String, Entry> flags, Map<String, SegmentEntry> segments) {
    this.flags = flags;
    this.segments = segments;
  }

  Entry flag(String key) {
    return flags.get(key);
  }

  SegmentEntry segment(String key) {
    return segments.get(key);
  }

  /**
   * The version of a flag that is not malformed in this ruleset.
   *
   * @param key the flag key
   * @return the version, or null when the ruleset has no such flag, the flag is malformed, a
   *     segment it names is missing or malformed, or the version does not fit a signed 64-bit
   *     integer
   */
  public Long flagVersion(String key) {
    Entry entry = flags.get(key);
    return entry != null && entry.status() == Model.READY ? entry.flag().version() : null;
  }

  /**
   * The number of flags the ruleset holds, malformed ones included.
   *
   * @return the count
   */
  public int flagCount() {
    return flags.size();
  }

  private static <T> Map<String, T> indexByKey(Object items, Function<Object, T> compile) {
    Map<String, T> index = new HashMap<>();
    if (items instanceof List<?> list) {
      for (Object item : list) {
        Object key = Values.isRecord(item) ? Values.record(item).get("key") : null;
        if (key instanceof String k && !index.containsKey(k)) {
          index.put(k, compile.apply(item));
        }
      }
    }
    return index;
  }

  private static ErrorCode segmentProblem(Flag flag, Map<String, SegmentEntry> segments) {
    for (Model.Rule rule : flag.rules()) {
      for (Model.Clause clause : rule.clauses()) {
        if (clause.op() != Model.Op.SEGMENT_MATCH) {
          continue;
        }
        for (Object key : clause.values()) {
          SegmentEntry segment = segments.get((String) key);
          if (segment == null) {
            return ErrorCode.SEGMENT_NOT_FOUND;
          }
          if (segment.segment() == null) {
            return ErrorCode.MALFORMED_SEGMENT;
          }
        }
      }
    }
    return null;
  }

  private static Map<String, Map<String, TargetHit>> targetIndex(Flag flag) {
    Map<String, Map<String, TargetHit>> index = new HashMap<>();
    List<Target> targets = flag.targets();
    for (int order = 0; order < targets.size(); order++) {
      Target target = targets.get(order);
      String kind = target.contextKind() == null ? Contexts.DEFAULT_KIND : target.contextKind();
      Map<String, TargetHit> keys = index.computeIfAbsent(kind, k -> new HashMap<>());
      for (String contextKey : target.contextKeys()) {
        keys.putIfAbsent(contextKey, new TargetHit(order, target.variation()));
      }
    }
    return index;
  }

  private static Entry compileFlag(Object value, Map<String, SegmentEntry> segments) {
    Header header = Parse.header(value);
    if (header == null) {
      return new Entry(Model.MALFORMED, null, null, null, null);
    }
    Flag flag = Parse.flag(header, value);
    if (flag == null) {
      return new Entry(Model.BROKEN, header, null, ErrorCode.MALFORMED_FLAG, null);
    }
    ErrorCode code = segmentProblem(flag, segments);
    if (code != null) {
      return new Entry(Model.BROKEN, header, null, code, null);
    }
    return new Entry(Model.READY, header, flag, null, targetIndex(flag));
  }

  /**
   * Validates and indexes a ruleset. It never throws for bad content: a bad flag becomes an error
   * result when it is evaluated.
   *
   * @param input the parsed JSON, from a plain tree of maps, lists, strings, doubles and booleans
   * @return the ruleset
   */
  public static Ruleset create(Object input) {
    Object rawFlags = null;
    Object rawSegments = null;
    if (Values.isRecord(input)) {
      rawFlags = Values.record(input).get("flags");
      rawSegments = Values.record(input).get("segments");
    }
    Map<String, SegmentEntry> segments =
        indexByKey(rawSegments, item -> new SegmentEntry(Parse.segment(item)));
    Map<String, Entry> flags = indexByKey(rawFlags, item -> compileFlag(item, segments));
    return new Ruleset(flags, segments);
  }

  /**
   * Whether a ruleset's JSON has both arrays, as a 200 body must.
   *
   * @param input the parsed body
   * @return true when {@code flags} and {@code segments} are both arrays
   */
  public static boolean hasArrays(Object input) {
    return Values.isRecord(input)
        && Values.record(input).get("flags") instanceof List
        && Values.record(input).get("segments") instanceof List;
  }
}
