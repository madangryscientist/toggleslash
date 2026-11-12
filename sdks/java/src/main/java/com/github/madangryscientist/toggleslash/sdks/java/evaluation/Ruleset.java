package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.AttributeClause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Clause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Flag;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Header;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Op;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Prerequisite;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Rollout;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Rule;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Segment;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.SegmentClause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.SegmentRule;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Serve;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Target;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.TargetHit;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.OptionalLong;
import java.util.Set;

/**
 * A ruleset: one environment's flags and segments, validated once at load. Loading never fails: a
 * bad flag becomes an {@code ERROR} result when it is evaluated. Immutable, so it is safe to share
 * between threads.
 */
public final class Ruleset {

  /** The state of a flag entry after loading. */
  enum Status {
    MALFORMED,
    BROKEN,
    READY
  }

  /**
   * A flag as loaded. {@code header} is null for {@code MALFORMED}; {@code flag} is set for {@code
   * READY}; {@code code} is the error of a {@code BROKEN} flag; {@code version} is -1 unless the
   * flag's body is valid.
   */
  record FlagEntry(Status status, Header header, Flag flag, ErrorCode code, long version) {}

  /** A segment entry: {@code segment} is null when the segment is malformed. */
  record SegmentEntry(Segment segment) {}

  private static final Ruleset EMPTY = new Ruleset(Map.of(), Map.of());

  private static final class Invalid extends RuntimeException {
    private static final long serialVersionUID = 1L;

    Invalid() {
      super(null, null, false, false);
    }
  }

  private static final int BUCKET_COUNT = 100_000;

  private final Map<String, FlagEntry> flags;
  private final Map<String, SegmentEntry> segments;

  private Ruleset(Map<String, FlagEntry> flags, Map<String, SegmentEntry> segments) {
    this.flags = flags;
    this.segments = segments;
  }

  /** The empty ruleset. */
  public static Ruleset empty() {
    return EMPTY;
  }

  /**
   * Loads a ruleset from a parsed JSON value (see {@link Json}). A value that is not an object, or
   * a {@code flags} or {@code segments} that is not an array, counts as empty.
   */
  public static Ruleset load(Object input) {
    Object rawFlags = null;
    Object rawSegments = null;
    if (input instanceof Map<?, ?> map) {
      rawFlags = map.get("flags");
      rawSegments = map.get("segments");
    }
    Map<String, SegmentEntry> segments = new HashMap<>();
    for (Object item : asList(rawSegments)) {
      String key = keyOf(item);
      if (key != null && !segments.containsKey(key)) {
        segments.put(key, new SegmentEntry(compileSegment((Map<?, ?>) item)));
      }
    }
    Map<String, FlagEntry> flags = new HashMap<>();
    for (Object item : asList(rawFlags)) {
      String key = keyOf(item);
      if (key != null && !flags.containsKey(key)) {
        flags.put(key, compileFlag((Map<?, ?>) item, segments));
      }
    }
    return new Ruleset(flags, segments);
  }

  /** How many flags the ruleset holds (distinct keys). */
  public int flagCount() {
    return flags.size();
  }

  /**
   * The flag's {@code version}, or empty when the ruleset has no such flag, the flag is malformed,
   * or the version is above the signed 64-bit range.
   */
  public OptionalLong flagVersion(String flagKey) {
    FlagEntry entry = flags.get(flagKey);
    if (entry == null || entry.version() < 0) {
      return OptionalLong.empty();
    }
    return OptionalLong.of(entry.version());
  }

  FlagEntry flag(String key) {
    return flags.get(key);
  }

  Segment segment(String key) {
    SegmentEntry entry = segments.get(key);
    return entry == null ? null : entry.segment();
  }

  boolean hasSegment(String key) {
    return segments.containsKey(key);
  }

  // ---- loading ----

  private static List<?> asList(Object value) {
    return value instanceof List<?> list ? list : List.of();
  }

  private static String keyOf(Object item) {
    return item instanceof Map<?, ?> map && map.get("key") instanceof String key ? key : null;
  }

  private static FlagEntry compileFlag(Map<?, ?> value, Map<String, SegmentEntry> segments) {
    Header header;
    try {
      header = readHeader(value);
    } catch (Invalid e) {
      return new FlagEntry(Status.MALFORMED, null, null, ErrorCode.MALFORMED_FLAG, -1);
    }
    Flag flag;
    try {
      flag = readBody(value, header);
    } catch (Invalid e) {
      return new FlagEntry(Status.BROKEN, header, null, ErrorCode.MALFORMED_FLAG, -1);
    }
    long version = versionOf(value.get("version"));
    ErrorCode code = segmentProblem(flag, segments);
    if (code != null) {
      return new FlagEntry(Status.BROKEN, header, null, code, version);
    }
    return new FlagEntry(Status.READY, header, flag, null, version);
  }

  /** The version as a signed 64-bit value, or -1 above that range. */
  private static long versionOf(Object value) {
    double d = ((Number) value).doubleValue();
    return d >= 9.223372036854775807E18 ? -1 : (long) d;
  }

  /**
   * The first reference to a segment that is missing or malformed, in rule, clause, value order.
   */
  private static ErrorCode segmentProblem(Flag flag, Map<String, SegmentEntry> segments) {
    for (Rule rule : flag.rules()) {
      for (Clause clause : rule.clauses()) {
        if (!(clause instanceof SegmentClause segmentClause)) {
          continue;
        }
        for (String key : segmentClause.values()) {
          SegmentEntry entry = segments.get(key);
          if (entry == null) {
            return ErrorCode.SEGMENT_NOT_FOUND;
          }
          if (entry.segment() == null) {
            return ErrorCode.MALFORMED_SEGMENT;
          }
        }
      }
    }
    return null;
  }

  private static boolean isCount(Object value) {
    if (!(value instanceof Number number)) {
      return false;
    }
    double d = number.doubleValue();
    return Double.isFinite(d) && d == Math.floor(d) && d >= 0;
  }

  /** A count that is a valid index below {@code limit}; returns it as an int. */
  private static int index(Object value, int limit) {
    if (!isCount(value) || ((Number) value).doubleValue() >= limit) {
      throw new Invalid();
    }
    return (int) ((Number) value).doubleValue();
  }

  private static String key(Object value) {
    if (value instanceof String text && !text.isEmpty()) {
      return text;
    }
    throw new Invalid();
  }

  private static Map<?, ?> record(Object value) {
    if (value instanceof Map<?, ?> map) {
      return map;
    }
    throw new Invalid();
  }

  private static List<?> list(Object value) {
    if (value instanceof List<?> list) {
      return list;
    }
    throw new Invalid();
  }

  private static List<String> strings(Object value) {
    List<String> out = new ArrayList<>();
    for (Object item : list(value)) {
      if (!(item instanceof String text)) {
        throw new Invalid();
      }
      out.add(text);
    }
    return out;
  }

  private static String optionalKind(Object value) {
    if (value == null) {
      return null;
    }
    if (!Contexts.isKind(value)) {
      throw new Invalid();
    }
    return (String) value;
  }

  private static boolean negate(Object value) {
    if (value == null) {
      return false;
    }
    if (value instanceof Boolean flag) {
      return flag;
    }
    throw new Invalid();
  }

  /**
   * What serving the off variation needs. It is checked apart so {@code on: false} survives a bad
   * rule.
   */
  private static Header readHeader(Map<?, ?> value) {
    if (!(value.get("on") instanceof Boolean on)) {
      throw new Invalid();
    }
    String key = key(value.get("key"));
    List<?> variations = list(value.get("variations"));
    if (variations.isEmpty()) {
      throw new Invalid();
    }
    Object off = value.get("offVariation");
    int offVariation = off == null ? -1 : index(off, variations.size());
    return new Header(key, on, new ArrayList<>(variations), offVariation);
  }

  private static Flag readBody(Map<?, ?> value, Header header) {
    int count = header.variations().size();
    if (!(value.get("salt") instanceof String salt) || !isCount(value.get("version"))) {
      throw new Invalid();
    }
    List<Target> targets = new ArrayList<>();
    for (Object item : list(value.get("targets"))) {
      Map<?, ?> target = record(item);
      targets.add(
          new Target(
              index(target.get("variation"), count),
              strings(target.get("contextKeys")),
              optionalKind(target.get("contextKind"))));
    }
    List<Prerequisite> prerequisites = new ArrayList<>();
    for (Object item : list(value.get("prerequisites"))) {
      Map<?, ?> prerequisite = record(item);
      if (!isCount(prerequisite.get("variation"))) {
        throw new Invalid();
      }
      double variation = ((Number) prerequisite.get("variation")).doubleValue();
      prerequisites.add(
          new Prerequisite(
              key(prerequisite.get("flagKey")), (int) Math.min(variation, Integer.MAX_VALUE)));
    }
    List<Rule> rules = new ArrayList<>();
    for (Object item : list(value.get("rules"))) {
      Map<?, ?> rule = record(item);
      List<Clause> clauses = new ArrayList<>();
      for (Object clause : list(rule.get("clauses"))) {
        clauses.add(readClause(clause, true));
      }
      rules.add(new Rule(clauses, readServe(rule, count)));
    }
    Serve fallthrough = readServe(record(value.get("fallthrough")), count);
    return new Flag(
        header,
        salt,
        versionOf(value.get("version")),
        prerequisites,
        rules,
        fallthrough,
        targetIndex(targets));
  }

  private static Map<String, Map<String, TargetHit>> targetIndex(List<Target> targets) {
    Map<String, Map<String, TargetHit>> index = new HashMap<>();
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

  private static Serve readServe(Map<?, ?> value, int count) {
    Object rollout = value.get("rollout");
    Object variation = value.get("variation");
    if (rollout == null) {
      return new Serve(index(variation, count), null);
    }
    if (variation != null) {
      throw new Invalid();
    }
    return new Serve(-1, readRollout(rollout, count));
  }

  private static Rollout readRollout(Object value, int count) {
    Map<?, ?> rollout = record(value);
    String kind = optionalKind(rollout.get("contextKind"));
    List<?> items = list(rollout.get("variations"));
    if (items.isEmpty()) {
      throw new Invalid();
    }
    int[] variations = new int[items.size()];
    int[] weights = new int[items.size()];
    long total = 0;
    for (int i = 0; i < items.size(); i++) {
      Map<?, ?> item = record(items.get(i));
      variations[i] = index(item.get("variation"), count);
      weights[i] = index(item.get("weight"), BUCKET_COUNT + 1);
      total += weights[i];
    }
    if (total != BUCKET_COUNT) {
      throw new Invalid();
    }
    return new Rollout(variations, weights, kind);
  }

  private static Clause readClause(Object value, boolean allowSegment) {
    Map<?, ?> clause = record(value);
    if (allowSegment && "segmentMatch".equals(clause.get("op"))) {
      return new SegmentClause(strings(clause.get("values")), negate(clause.get("negate")));
    }
    return readAttributeClause(clause);
  }

  private static AttributeClause readAttributeClause(Map<?, ?> clause) {
    Op op = Op.of(clause.get("op"));
    if (op == null) {
      throw new Invalid();
    }
    List<String> components = attributeName(clause.get("attribute"));
    boolean negate = negate(clause.get("negate"));
    String kind = optionalKind(clause.get("contextKind"));
    List<Object> values = new ArrayList<>();
    for (Object item : list(clause.get("values"))) {
      if (item instanceof Number number) {
        double d = number.doubleValue();
        if (!Double.isFinite(d)) {
          throw new Invalid();
        }
        values.add(Double.valueOf(d));
      } else if (item instanceof String || item instanceof Boolean) {
        values.add(item);
      } else {
        throw new Invalid();
      }
    }
    PatternProgram[] patterns = null;
    if (op == Op.MATCHES) {
      patterns = new PatternProgram[values.size()];
      for (int i = 0; i < patterns.length; i++) {
        if (values.get(i) instanceof String text) {
          patterns[i] = PatternProgram.compile(text);
        }
      }
    }
    return new AttributeClause(op, components, values, patterns, negate, kind);
  }

  /** A literal name is one component; a {@code /} reference is its decoded components. */
  private static List<String> attributeName(Object value) {
    String name = key(value);
    if (!name.startsWith("/")) {
      return List.of(name);
    }
    List<String> components = new ArrayList<>();
    int start = 1;
    while (true) {
      int slash = name.indexOf('/', start);
      String part = slash < 0 ? name.substring(start) : name.substring(start, slash);
      components.add(decodePart(part));
      if (slash < 0) {
        return components;
      }
      start = slash + 1;
    }
  }

  /** {@code ~1} is {@code /} and {@code ~0} is {@code ~}, in one left-to-right pass. */
  private static String decodePart(String part) {
    if (part.isEmpty()) {
      throw new Invalid();
    }
    StringBuilder out = new StringBuilder();
    for (int i = 0; i < part.length(); i++) {
      char c = part.charAt(i);
      if (c != '~') {
        out.append(c);
        continue;
      }
      char next = i + 1 < part.length() ? part.charAt(i + 1) : 0;
      if (next == '0') {
        out.append('~');
      } else if (next == '1') {
        out.append('/');
      } else {
        throw new Invalid();
      }
      i++;
    }
    return out.toString();
  }

  private static Segment compileSegment(Map<?, ?> value) {
    try {
      String key = key(value.get("key"));
      if (!(value.get("salt") instanceof String salt)) {
        throw new Invalid();
      }
      Map<String, Set<String>> included = keysByKind(strings(value.get("included")));
      Map<String, Set<String>> excluded = keysByKind(strings(value.get("excluded")));
      addLists(included, value.get("includedContexts"));
      addLists(excluded, value.get("excludedContexts"));
      List<SegmentRule> rules = new ArrayList<>();
      for (Object item : list(value.get("rules"))) {
        rules.add(readSegmentRule(record(item)));
      }
      return new Segment(key, salt, included, excluded, rules);
    } catch (Invalid e) {
      return null;
    }
  }

  private static Map<String, Set<String>> keysByKind(List<String> userKeys) {
    Map<String, Set<String>> index = new HashMap<>();
    index.put(Contexts.DEFAULT_KIND, new HashSet<>(userKeys));
    return index;
  }

  private static void addLists(Map<String, Set<String>> index, Object lists) {
    if (lists == null) {
      return;
    }
    for (Object item : list(lists)) {
      Map<?, ?> entry = record(item);
      Object kind = entry.get("contextKind");
      if (!Contexts.isKind(kind)) {
        throw new Invalid();
      }
      List<String> values = strings(entry.get("values"));
      index.computeIfAbsent((String) kind, k -> new HashSet<>()).addAll(values);
    }
  }

  private static SegmentRule readSegmentRule(Map<?, ?> rule) {
    Object weight = rule.get("weight");
    int w = weight == null ? -1 : index(weight, BUCKET_COUNT + 1);
    String kind = optionalKind(rule.get("rolloutContextKind"));
    List<AttributeClause> clauses = new ArrayList<>();
    for (Object clause : list(rule.get("clauses"))) {
      clauses.add((AttributeClause) readClause(clause, false));
    }
    return new SegmentRule(clauses, w, kind);
  }
}
