package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * A ruleset validated and indexed once, so each evaluation skips that work. {@link #parse} never
 * throws: a bad flag becomes an {@code ERROR} result when it is evaluated. {@code
 * docs/evaluation.md} is normative for what counts as malformed. Null and absent are the same for
 * an optional field.
 */
public final class Ruleset {
  // ---- the model ----

  /** The ops, in the spelling of the spec. */
  enum Op {
    IN("in"),
    CONTAINS("contains"),
    STARTS_WITH("startsWith"),
    ENDS_WITH("endsWith"),
    LT("lt"),
    LTE("lte"),
    GT("gt"),
    GTE("gte"),
    SEMVER_EQ("semverEq"),
    SEMVER_LT("semverLt"),
    SEMVER_LTE("semverLte"),
    SEMVER_GT("semverGt"),
    SEMVER_GTE("semverGte"),
    BEFORE("before"),
    AFTER("after"),
    MATCHES("matches"),
    SEGMENT_MATCH("segmentMatch");

    private final String wire;

    Op(String wire) {
      this.wire = wire;
    }

    static Op of(Object text) {
      for (Op op : values()) {
        if (op.wire.equals(text)) {
          return op;
        }
      }
      return null;
    }
  }

  /** A clause: an attribute test, or a segmentMatch. */
  static final class Clause {
    final Op op;
    final String attribute;

    /** Doubles (finite), strings and booleans. */
    final List<Object> values;

    final boolean negate;
    final String contextKind;

    /** One compiled pattern per value for {@code matches} (null for an invalid one), else null. */
    final Pattern[] patterns;

    /** The segment keys of a segmentMatch. */
    final List<String> segmentKeys;

    Clause(
        Op op,
        String attribute,
        List<Object> values,
        boolean negate,
        String contextKind,
        List<String> segmentKeys) {
      this.op = op;
      this.attribute = attribute;
      this.values = values;
      this.negate = negate;
      this.contextKind = contextKind;
      this.segmentKeys = segmentKeys;
      if (op == Op.MATCHES) {
        this.patterns = new Pattern[values.size()];
        for (int i = 0; i < patterns.length; i++) {
          Object value = values.get(i);
          patterns[i] = value instanceof String ? Pattern.compile((String) value) : null;
        }
      } else {
        this.patterns = null;
      }
    }
  }

  /** One weighted variation of a rollout. */
  record Weighted(int variation, int weight) {}

  /** A rollout. */
  record Rollout(String contextKind, List<Weighted> variations) {}

  /** A variation, or a rollout. */
  record Serve(int variation, Rollout rollout) {}

  /** A rule: clauses, then what it serves. */
  record Rule(List<Clause> clauses, Serve serve) {}

  /** A target. */
  record Target(int variation, List<String> contextKeys, String contextKind) {}

  /** A prerequisite. {@code variation} is a count, which can be larger than any variation index. */
  record Prerequisite(String flagKey, double variation) {}

  /** The part of a flag that serving the off variation needs. */
  record Header(String key, boolean on, List<Object> variations, Integer offVariation) {}

  /** A flag that fits the spec. */
  static final class Flag {
    Header header;
    String salt;
    double version;
    List<Target> targets;
    List<Prerequisite> prerequisites;
    List<Rule> rules;
    Serve fallthrough;

    /** Targets by kind, then by key; the first target in array order holds each key. */
    Map<String, Map<String, int[]>> targetIndex;
  }

  /** A flag entry: malformed, broken (valid header, a code) or ready. */
  static final class Entry {
    final Header header;
    final ErrorCode brokenCode;
    final Flag flag;

    Entry(Header header, ErrorCode brokenCode, Flag flag) {
      this.header = header;
      this.brokenCode = brokenCode;
      this.flag = flag;
    }

    boolean isMalformed() {
      return header == null;
    }

    boolean isBroken() {
      return header != null && flag == null;
    }
  }

  /** A segment rule. */
  record SegmentRule(List<Clause> clauses, Integer weight, String rolloutContextKind) {}

  /** A segment that fits the spec, with its key lists by kind. */
  static final class ReadySegment {
    String key;
    String salt;
    List<SegmentRule> rules;
    Map<String, Set<String>> included;
    Map<String, Set<String>> excluded;
  }

  private static final ReadySegment MALFORMED_SEGMENT = new ReadySegment();

  final Map<String, Entry> flags;
  final Map<String, ReadySegment> segments;

  private Ruleset(Map<String, Entry> flags, Map<String, ReadySegment> segments) {
    this.flags = flags;
    this.segments = segments;
  }

  static boolean isMalformed(ReadySegment segment) {
    return segment == MALFORMED_SEGMENT;
  }

  /** The number of flags the ruleset indexes, malformed ones included. */
  public int flagCount() {
    return flags.size();
  }

  /**
   * The flag's {@code version}, or null when the ruleset has no such flag or the flag is malformed
   * (its header or its body does not fit the spec).
   */
  public Double flagVersion(String key) {
    Entry entry = flags.get(key);
    if (entry == null || entry.flag == null) {
      return null;
    }
    return entry.flag.version;
  }

  // ---- reading ----

  private static boolean isCount(Object value) {
    if (!(value instanceof Double)) {
      return false;
    }
    double d = (Double) value;
    return !Double.isInfinite(d) && !Double.isNaN(d) && d == Math.rint(d) && d >= 0;
  }

  private static boolean isKey(Object value) {
    return value instanceof String && !((String) value).isEmpty();
  }

  private static Integer variationIndex(Object value, int count) {
    if (!isCount(value) || (Double) value >= count) {
      return null;
    }
    return (int) ((Double) value).doubleValue();
  }

  private static List<String> stringList(Object value) {
    if (!(value instanceof List<?>)) {
      return null;
    }
    List<String> out = new ArrayList<>();
    for (Object item : (List<?>) value) {
      if (!(item instanceof String)) {
        return null;
      }
      out.add((String) item);
    }
    return out;
  }

  private static boolean isOptionalKind(Object value) {
    return value == null || Contexts.isKind(value);
  }

  private static boolean isNegate(Object value) {
    return value == null || value instanceof Boolean;
  }

  private static boolean isAttributeName(Object value) {
    if (!isKey(value)) {
      return false;
    }
    String text = (String) value;
    return !text.startsWith("/") || References.parse(text) != null;
  }

  private static boolean isClauseValue(Object value) {
    if (value instanceof Double) {
      return !Double.isInfinite((Double) value) && !Double.isNaN((Double) value);
    }
    return value instanceof String || value instanceof Boolean;
  }

  private static Clause attributeClause(Object value) {
    if (!(value instanceof Map<?, ?>)) {
      return null;
    }
    Map<?, ?> map = (Map<?, ?>) value;
    Op op = Op.of(map.get("op"));
    if (op == null || op == Op.SEGMENT_MATCH) {
      return null;
    }
    Object values = map.get("values");
    if (!isAttributeName(map.get("attribute"))
        || !isNegate(map.get("negate"))
        || !isOptionalKind(map.get("contextKind"))
        || !(values instanceof List<?>)) {
      return null;
    }
    List<Object> checked = new ArrayList<>();
    for (Object item : (List<?>) values) {
      if (!isClauseValue(item)) {
        return null;
      }
      checked.add(item);
    }
    return new Clause(
        op,
        (String) map.get("attribute"),
        checked,
        Boolean.TRUE.equals(map.get("negate")),
        (String) map.get("contextKind"),
        null);
  }

  private static Clause segmentClause(Object value) {
    if (!(value instanceof Map<?, ?>)) {
      return null;
    }
    Map<?, ?> map = (Map<?, ?>) value;
    if (!"segmentMatch".equals(map.get("op")) || !isNegate(map.get("negate"))) {
      return null;
    }
    List<String> keys = stringList(map.get("values"));
    if (keys == null) {
      return null;
    }
    return new Clause(
        Op.SEGMENT_MATCH, null, List.of(), Boolean.TRUE.equals(map.get("negate")), null, keys);
  }

  private static Rollout rollout(Object value, int variationCount) {
    if (!(value instanceof Map<?, ?>)) {
      return null;
    }
    Map<?, ?> map = (Map<?, ?>) value;
    if (!isOptionalKind(map.get("contextKind"))) {
      return null;
    }
    Object variations = map.get("variations");
    if (!(variations instanceof List<?>) || ((List<?>) variations).isEmpty()) {
      return null;
    }
    List<Weighted> out = new ArrayList<>();
    double total = 0;
    for (Object item : (List<?>) variations) {
      if (!(item instanceof Map<?, ?>)) {
        return null;
      }
      Map<?, ?> entry = (Map<?, ?>) item;
      Integer variation = variationIndex(entry.get("variation"), variationCount);
      Object weight = entry.get("weight");
      if (variation == null || !isCount(weight) || (Double) weight > Bucket.COUNT) {
        return null;
      }
      out.add(new Weighted(variation, (int) ((Double) weight).doubleValue()));
      total += (Double) weight;
    }
    if (total != Bucket.COUNT) {
      return null;
    }
    return new Rollout((String) map.get("contextKind"), out);
  }

  private static Serve serve(Object value, int variationCount) {
    if (!(value instanceof Map<?, ?>)) {
      return null;
    }
    Map<?, ?> map = (Map<?, ?>) value;
    Object variation = map.get("variation");
    Object rollout = map.get("rollout");
    if (rollout == null) {
      Integer index = variationIndex(variation, variationCount);
      return index == null ? null : new Serve(index, null);
    }
    if (variation != null) {
      return null;
    }
    Rollout parsed = rollout(rollout, variationCount);
    return parsed == null ? null : new Serve(-1, parsed);
  }

  private static Rule rule(Object value, int variationCount) {
    if (!(value instanceof Map<?, ?>)) {
      return null;
    }
    Object clauses = ((Map<?, ?>) value).get("clauses");
    if (!(clauses instanceof List<?>)) {
      return null;
    }
    List<Clause> out = new ArrayList<>();
    for (Object item : (List<?>) clauses) {
      Clause clause = attributeClause(item);
      if (clause == null) {
        clause = segmentClause(item);
      }
      if (clause == null) {
        return null;
      }
      out.add(clause);
    }
    Serve serve = serve(value, variationCount);
    return serve == null ? null : new Rule(out, serve);
  }

  private static Target target(Object value, int variationCount) {
    if (!(value instanceof Map<?, ?>)) {
      return null;
    }
    Map<?, ?> map = (Map<?, ?>) value;
    Integer variation = variationIndex(map.get("variation"), variationCount);
    List<String> keys = stringList(map.get("contextKeys"));
    if (variation == null || keys == null || !isOptionalKind(map.get("contextKind"))) {
      return null;
    }
    return new Target(variation, keys, (String) map.get("contextKind"));
  }

  private static Prerequisite prerequisite(Object value) {
    if (!(value instanceof Map<?, ?>)) {
      return null;
    }
    Map<?, ?> map = (Map<?, ?>) value;
    if (!isKey(map.get("flagKey")) || !isCount(map.get("variation"))) {
      return null;
    }
    return new Prerequisite((String) map.get("flagKey"), (Double) map.get("variation"));
  }

  /**
   * What serving the off variation needs; checked apart so {@code on: false} survives a bad rule.
   */
  private static Header header(Object value) {
    if (!(value instanceof Map<?, ?>)) {
      return null;
    }
    Map<?, ?> map = (Map<?, ?>) value;
    Object variations = map.get("variations");
    if (!(map.get("on") instanceof Boolean)
        || !isKey(map.get("key"))
        || !(variations instanceof List<?>)
        || ((List<?>) variations).isEmpty()) {
      return null;
    }
    List<Object> list = new ArrayList<>((List<?>) variations);
    Object off = map.get("offVariation");
    Integer offIndex = null;
    if (off != null) {
      offIndex = variationIndex(off, list.size());
      if (offIndex == null) {
        return null;
      }
    }
    return new Header((String) map.get("key"), (Boolean) map.get("on"), list, offIndex);
  }

  /** The body of a flag, or null when it does not fit the spec. */
  private static Flag body(Map<?, ?> map, Header header) {
    int count = header.variations().size();
    Object salt = map.get("salt");
    if (!(salt instanceof String) || !isCount(map.get("version"))) {
      return null;
    }
    Flag flag = new Flag();
    flag.header = header;
    flag.salt = (String) salt;
    flag.version = (Double) map.get("version");
    flag.targets = new ArrayList<>();
    flag.prerequisites = new ArrayList<>();
    flag.rules = new ArrayList<>();
    if (!(map.get("targets") instanceof List<?>)
        || !(map.get("prerequisites") instanceof List<?>)
        || !(map.get("rules") instanceof List<?>)) {
      return null;
    }
    for (Object item : (List<?>) map.get("targets")) {
      Target target = target(item, count);
      if (target == null) {
        return null;
      }
      flag.targets.add(target);
    }
    for (Object item : (List<?>) map.get("prerequisites")) {
      Prerequisite prerequisite = prerequisite(item);
      if (prerequisite == null) {
        return null;
      }
      flag.prerequisites.add(prerequisite);
    }
    for (Object item : (List<?>) map.get("rules")) {
      Rule rule = rule(item, count);
      if (rule == null) {
        return null;
      }
      flag.rules.add(rule);
    }
    flag.fallthrough = serve(map.get("fallthrough"), count);
    return flag.fallthrough == null ? null : flag;
  }

  private static Map<String, Map<String, int[]>> targetIndex(Flag flag) {
    Map<String, Map<String, int[]>> index = new HashMap<>();
    for (int order = 0; order < flag.targets.size(); order++) {
      Target target = flag.targets.get(order);
      String kind = target.contextKind() == null ? Contexts.DEFAULT_KIND : target.contextKind();
      Map<String, int[]> keys = index.computeIfAbsent(kind, k -> new HashMap<>());
      for (String contextKey : target.contextKeys()) {
        keys.putIfAbsent(contextKey, new int[] {order, target.variation()});
      }
    }
    return index;
  }

  // ---- segments ----

  private static SegmentRule segmentRule(Object value) {
    if (!(value instanceof Map<?, ?>)) {
      return null;
    }
    Map<?, ?> map = (Map<?, ?>) value;
    Object weight = map.get("weight");
    if (weight != null && (!isCount(weight) || (Double) weight > Bucket.COUNT)) {
      return null;
    }
    if (!isOptionalKind(map.get("rolloutContextKind"))
        || !(map.get("clauses") instanceof List<?>)) {
      return null;
    }
    List<Clause> clauses = new ArrayList<>();
    for (Object item : (List<?>) map.get("clauses")) {
      Clause clause = attributeClause(item);
      if (clause == null) {
        return null;
      }
      clauses.add(clause);
    }
    Integer parsedWeight = weight == null ? null : (int) ((Double) weight).doubleValue();
    return new SegmentRule(clauses, parsedWeight, (String) map.get("rolloutContextKind"));
  }

  /** Context lists of other kinds: kind then keys; null when they do not fit the spec. */
  private static boolean addContextLists(Object lists, Map<String, Set<String>> into) {
    if (lists == null) {
      return true;
    }
    if (!(lists instanceof List<?>)) {
      return false;
    }
    for (Object item : (List<?>) lists) {
      if (!(item instanceof Map<?, ?>)) {
        return false;
      }
      Map<?, ?> map = (Map<?, ?>) item;
      List<String> values = stringList(map.get("values"));
      if (!Contexts.isKind(map.get("contextKind")) || values == null) {
        return false;
      }
      into.computeIfAbsent((String) map.get("contextKind"), k -> new HashSet<>()).addAll(values);
    }
    return true;
  }

  private static ReadySegment segment(Object value) {
    if (!(value instanceof Map<?, ?>)) {
      return MALFORMED_SEGMENT;
    }
    Map<?, ?> map = (Map<?, ?>) value;
    List<String> included = stringList(map.get("included"));
    List<String> excluded = stringList(map.get("excluded"));
    if (!isKey(map.get("key"))
        || !(map.get("salt") instanceof String)
        || included == null
        || excluded == null
        || !(map.get("rules") instanceof List<?>)) {
      return MALFORMED_SEGMENT;
    }
    ReadySegment ready = new ReadySegment();
    ready.key = (String) map.get("key");
    ready.salt = (String) map.get("salt");
    ready.included = new HashMap<>();
    ready.included.put(Contexts.DEFAULT_KIND, new HashSet<>(included));
    ready.excluded = new HashMap<>();
    ready.excluded.put(Contexts.DEFAULT_KIND, new HashSet<>(excluded));
    if (!addContextLists(map.get("includedContexts"), ready.included)
        || !addContextLists(map.get("excludedContexts"), ready.excluded)) {
      return MALFORMED_SEGMENT;
    }
    ready.rules = new ArrayList<>();
    for (Object item : (List<?>) map.get("rules")) {
      SegmentRule rule = segmentRule(item);
      if (rule == null) {
        return MALFORMED_SEGMENT;
      }
      ready.rules.add(rule);
    }
    return ready;
  }

  /**
   * The first reference to a segment that is missing or malformed, in rule, clause, value order.
   */
  private static ErrorCode segmentProblem(Flag flag, Map<String, ReadySegment> segments) {
    for (Rule rule : flag.rules) {
      for (Clause clause : rule.clauses()) {
        if (clause.op != Op.SEGMENT_MATCH) {
          continue;
        }
        for (String key : clause.segmentKeys) {
          ReadySegment segment = segments.get(key);
          if (segment == null) {
            return ErrorCode.SEGMENT_NOT_FOUND;
          }
          if (isMalformed(segment)) {
            return ErrorCode.MALFORMED_SEGMENT;
          }
        }
      }
    }
    return null;
  }

  private static Entry entry(Object value, Map<String, ReadySegment> segments) {
    Header header = header(value);
    if (header == null) {
      return new Entry(null, null, null);
    }
    Flag flag = body((Map<?, ?>) value, header);
    if (flag == null) {
      return new Entry(header, ErrorCode.MALFORMED_FLAG, null);
    }
    ErrorCode code = segmentProblem(flag, segments);
    if (code != null) {
      return new Entry(header, code, null);
    }
    flag.targetIndex = targetIndex(flag);
    return new Entry(header, null, flag);
  }

  /**
   * Validates and indexes a ruleset ({@code {flags, segments}} as the JSON parser hands it over). A
   * duplicate key keeps the first.
   */
  public static Ruleset parse(Object input) {
    Object rawFlags = null;
    Object rawSegments = null;
    if (input instanceof Map<?, ?>) {
      rawFlags = ((Map<?, ?>) input).get("flags");
      rawSegments = ((Map<?, ?>) input).get("segments");
    }
    Map<String, ReadySegment> segments = new HashMap<>();
    if (rawSegments instanceof List<?>) {
      for (Object item : (List<?>) rawSegments) {
        Object key = item instanceof Map<?, ?> ? ((Map<?, ?>) item).get("key") : null;
        if (key instanceof String && !segments.containsKey(key)) {
          segments.put((String) key, segment(item));
        }
      }
    }
    Map<String, Entry> flags = new HashMap<>();
    if (rawFlags instanceof List<?>) {
      for (Object item : (List<?>) rawFlags) {
        Object key = item instanceof Map<?, ?> ? ((Map<?, ?>) item).get("key") : null;
        if (key instanceof String && !flags.containsKey(key)) {
          flags.put((String) key, entry(item, segments));
        }
      }
    }
    return new Ruleset(flags, segments);
  }
}
