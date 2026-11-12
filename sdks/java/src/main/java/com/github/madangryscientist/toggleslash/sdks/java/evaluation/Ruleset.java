package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.ErrorCode;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Flag;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Header;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Segment;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

/**
 * A ruleset validated and indexed once, so each evaluation skips that work. Creating one never
 * throws: a bad flag becomes an {@code ERROR} result when it is evaluated.
 */
public final class Ruleset {
  /** What a flag is after validation. */
  enum Status {
    MALFORMED,
    BROKEN,
    READY
  }

  /**
   * A flag entry: malformed, broken (the header is fine, the body or a segment is not) or ready.
   */
  record Entry(Status status, Header header, ErrorCode code, Flag flag) {}

  /** A segment entry; {@code segment} is null when the segment is malformed. */
  record SegmentEntry(Segment segment) {}

  private final Map<String, Entry> flags;
  private final Map<String, SegmentEntry> segments;

  private Ruleset(Map<String, Entry> flags, Map<String, SegmentEntry> segments) {
    this.flags = flags;
    this.segments = segments;
  }

  /**
   * Validates and indexes a ruleset as JSON hands it over: a map with a {@code flags} and a {@code
   * segments} list. Anything else, or a field that is not a list, counts as empty.
   *
   * @param input the parsed JSON value
   * @return the ruleset
   */
  public static Ruleset of(Object input) {
    Object rawFlags = Values.isRecord(input) ? Values.get(input, "flags") : null;
    Object rawSegments = Values.isRecord(input) ? Values.get(input, "segments") : null;
    Map<String, SegmentEntry> segments = new HashMap<>();
    if (rawSegments instanceof List<?> items) {
      for (Object item : items) {
        if (Values.isRecord(item)
            && Values.get(item, "key") instanceof String key
            && !segments.containsKey(key)) {
          segments.put(key, new SegmentEntry(Compile.segment(item)));
        }
      }
    }
    Map<String, Entry> flags = new HashMap<>();
    if (rawFlags instanceof List<?> items) {
      for (Object item : items) {
        if (Values.isRecord(item)
            && Values.get(item, "key") instanceof String key
            && !flags.containsKey(key)) {
          flags.put(key, compileFlag(item, segments));
        }
      }
    }
    return new Ruleset(flags, segments);
  }

  private static Entry compileFlag(Object value, Map<String, SegmentEntry> segments) {
    Header header = Compile.header(value);
    if (header == null) {
      return new Entry(Status.MALFORMED, null, null, null);
    }
    Flag flag = Compile.flag(value, header);
    if (flag == null) {
      return new Entry(Status.BROKEN, header, ErrorCode.MALFORMED_FLAG, null);
    }
    ErrorCode code = segmentProblem(flag, segments);
    return code != null
        ? new Entry(Status.BROKEN, header, code, flag)
        : new Entry(Status.READY, header, null, flag);
  }

  /**
   * The first reference to a segment that is missing or malformed, in rule, clause, value order.
   */
  private static ErrorCode segmentProblem(Flag flag, Map<String, SegmentEntry> segments) {
    for (Model.Rule rule : flag.rules()) {
      for (Model.Clause clause : rule.clauses()) {
        if (clause instanceof Model.SegmentClause segmentClause) {
          for (String key : segmentClause.keys()) {
            SegmentEntry segment = segments.get(key);
            if (segment == null) {
              return ErrorCode.SEGMENT_NOT_FOUND;
            }
            if (segment.segment() == null) {
              return ErrorCode.MALFORMED_SEGMENT;
            }
          }
        }
      }
    }
    return null;
  }

  Entry flag(String key) {
    return flags.get(key);
  }

  SegmentEntry segment(String key) {
    return segments.get(key);
  }

  /**
   * The flag's {@code version}, or null when the ruleset has no such flag or the flag is malformed.
   * A version above 2 to the 63 minus 1 is reported as null.
   *
   * @param key the flag key
   * @return the version, or null
   */
  public Long flagVersion(String key) {
    Entry entry = flags.get(key);
    if (entry == null || entry.flag() == null) {
      return null;
    }
    double version = entry.flag().version();
    return version >= 9.223372036854775807E18 ? null : Long.valueOf((long) version);
  }

  /**
   * How many flags the ruleset holds.
   *
   * @return the count
   */
  public int flagCount() {
    return flags.size();
  }
}
