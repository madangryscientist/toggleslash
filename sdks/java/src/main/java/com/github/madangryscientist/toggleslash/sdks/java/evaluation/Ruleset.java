package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Broken;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Clause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Flag;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.FlagEntry;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.FlagHeader;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Malformed;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Op;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Ready;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Rule;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Segment;
import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import java.util.HashMap;
import java.util.Map;
import java.util.function.Function;

/**
 * One environment's flags and segments, validated and indexed once so each evaluation skips that
 * work. {@link #load(Json)} never throws: a bad flag becomes an error result when it is evaluated.
 * A ruleset is immutable and safe to share between threads.
 */
public final class Ruleset {
  private final Map<String, FlagEntry> flags;
  // A null segment is a malformed one.
  private final Map<String, Segment> segments;

  private Ruleset(Map<String, FlagEntry> flags, Map<String, Segment> segments) {
    this.flags = flags;
    this.segments = segments;
  }

  FlagEntry flag(String key) {
    return flags.get(key);
  }

  boolean hasSegment(String key) {
    return segments.containsKey(key);
  }

  Segment segment(String key) {
    return segments.get(key);
  }

  /**
   * The version of a flag that can be evaluated.
   *
   * @param key the flag key
   * @return its version, or null when the ruleset has no such flag, the flag is malformed, or the
   *     version is above 2^63 minus 1
   */
  public Long flagVersion(String key) {
    if (flags.get(key) instanceof Ready ready) {
      double version = ready.flag().version();
      return version < 9.2233720368547758E18 ? Long.valueOf((long) version) : null;
    }
    return null;
  }

  /**
   * Loads a ruleset from its JSON form.
   *
   * @param root the ruleset; one that is not an object is an empty ruleset
   * @return the ruleset
   */
  public static Ruleset load(Json root) {
    Json rawFlags = null;
    Json rawSegments = null;
    if (root instanceof Json.Obj obj) {
      rawFlags = obj.get("flags");
      rawSegments = obj.get("segments");
    }
    Map<String, Segment> segments = indexByKey(rawSegments, Loader::readSegment);
    Map<String, FlagEntry> flags = indexByKey(rawFlags, item -> compileFlag(item, segments));
    return new Ruleset(flags, segments);
  }

  /**
   * First entry per key wins, even when it is the malformed one; an entry with no string key is
   * dropped.
   */
  private static <T> Map<String, T> indexByKey(Json items, Function<Json.Obj, T> compile) {
    Map<String, T> index = new HashMap<>();
    if (!(items instanceof Json.Arr array)) {
      return index;
    }
    for (Json item : array.items()) {
      if (item instanceof Json.Obj obj && obj.get("key") instanceof Json.Str key) {
        if (!index.containsKey(key.value())) {
          index.put(key.value(), compile.apply(obj));
        }
      }
    }
    return index;
  }

  private static FlagEntry compileFlag(Json.Obj value, Map<String, Segment> segments) {
    FlagHeader header = Loader.readHeader(value);
    if (header == null) {
      return new Malformed();
    }
    Flag flag = Loader.readBody(value, header);
    if (flag == null) {
      return new Broken(header, ErrorCode.MALFORMED_FLAG);
    }
    ErrorCode problem = segmentProblem(flag, segments);
    return problem != null ? new Broken(header, problem) : new Ready(flag);
  }

  /**
   * The first reference to a segment that is missing or malformed, in rule, clause, value order.
   */
  private static ErrorCode segmentProblem(Flag flag, Map<String, Segment> segments) {
    for (Rule rule : flag.rules()) {
      for (Clause clause : rule.clauses()) {
        if (clause.op() != Op.SEGMENT_MATCH) {
          continue;
        }
        for (String key : clause.segmentKeys()) {
          if (!segments.containsKey(key)) {
            return ErrorCode.SEGMENT_NOT_FOUND;
          }
          if (segments.get(key) == null) {
            return ErrorCode.MALFORMED_SEGMENT;
          }
        }
      }
    }
    return null;
  }
}
