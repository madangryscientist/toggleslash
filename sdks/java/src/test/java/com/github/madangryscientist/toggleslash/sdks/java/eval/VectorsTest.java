package com.github.madangryscientist.toggleslash.sdks.java.eval;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.github.madangryscientist.toggleslash.sdks.java.Json;
import com.github.madangryscientist.toggleslash.sdks.java.Reason;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.stream.Stream;
import org.junit.jupiter.api.Test;

/** Runs every shared evaluation vector and every bucket pin, to fixtures/evaluation. */
class VectorsTest {
  private static final Path FIXTURES = Path.of("../../fixtures/evaluation");

  private static Map<String, Object> read(Path file) throws Exception {
    String text = Utf8.decode(Files.readAllBytes(file));
    return Values.record(Json.parse(text));
  }

  /** Equality by IEEE value, so 0 equals -0, over the plain JSON tree. */
  static boolean same(Object a, Object b) {
    if (a instanceof Double x && b instanceof Double y) {
      return x.doubleValue() == y.doubleValue();
    }
    if (a instanceof List<?> x && b instanceof List<?> y) {
      if (x.size() != y.size()) {
        return false;
      }
      for (int i = 0; i < x.size(); i++) {
        if (!same(x.get(i), y.get(i))) {
          return false;
        }
      }
      return true;
    }
    if (a instanceof Map<?, ?> x && b instanceof Map<?, ?> y) {
      if (!x.keySet().equals(y.keySet())) {
        return false;
      }
      for (Object key : x.keySet()) {
        if (!same(x.get(key), y.get(key))) {
          return false;
        }
      }
      return true;
    }
    return a == null ? b == null : a.equals(b);
  }

  private static String describe(Reason reason) {
    StringBuilder text = new StringBuilder(reason.kind().name());
    if (reason.ruleIndex() != null) {
      text.append(" ruleIndex=").append(reason.ruleIndex());
    }
    if (reason.flagKey() != null) {
      text.append(" flagKey=").append(reason.flagKey());
    }
    if (reason.errorCode() != null) {
      text.append(" code=").append(reason.errorCode());
    }
    return text.toString();
  }

  private static String describe(Map<String, Object> reason) {
    Reason as =
        new Reason(
            com.github.madangryscientist.toggleslash.sdks.java.ReasonKind.valueOf(
                (String) reason.get("kind")),
            reason.get("ruleIndex") == null ? null : ((Double) reason.get("ruleIndex")).intValue(),
            (String) reason.get("flagKey"),
            reason.get("code") == null
                ? null
                : com.github.madangryscientist.toggleslash.sdks.java.ErrorCode.valueOf(
                    (String) reason.get("code")));
    return describe(as);
  }

  @Test
  void everyVectorAndPin() throws Exception {
    List<Path> files;
    try (Stream<Path> listing = Files.list(FIXTURES)) {
      files = listing.filter(p -> p.toString().endsWith(".json")).sorted().toList();
    }
    int vectors = 0;
    int pins = 0;
    List<String> failures = new ArrayList<>();
    for (Path file : files) {
      Map<String, Object> parsed = read(file);
      if (file.getFileName().toString().equals("buckets.json")) {
        for (Object item : Values.list(parsed.get("buckets"))) {
          Map<String, Object> pin = Values.record(item);
          int got =
              Bucket.of(
                  (String) pin.get("flagKey"),
                  (String) pin.get("salt"),
                  (String) pin.get("contextKey"));
          pins++;
          if (got != ((Double) pin.get("bucket")).intValue()) {
            failures.add("pin " + pin + " gave " + got);
          }
        }
        continue;
      }
      for (Object item : Values.list(parsed.get("vectors"))) {
        Map<String, Object> vector = Values.record(item);
        vectors++;
        Map<String, Object> input = new java.util.LinkedHashMap<>();
        input.put("flags", vector.get("flags"));
        input.put("segments", vector.get("segments"));
        Evaluation got;
        try {
          got =
              Evaluator.evaluate(
                  Ruleset.create(input), (String) vector.get("flagKey"), vector.get("context"));
        } catch (RuntimeException e) {
          failures.add(file.getFileName() + ": " + vector.get("name") + " threw " + e);
          continue;
        }
        Map<String, Object> expected = Values.record(vector.get("expected"));
        Map<String, Object> reason = Values.record(expected.get("reason"));
        boolean ok =
            same(got.value(), expected.get("value"))
                && same(
                    got.variationIndex() == null ? null : got.variationIndex().doubleValue(),
                    expected.get("variationIndex"))
                && describe(got.reason()).equals(describe(reason));
        if (!ok) {
          failures.add(
              file.getFileName()
                  + ": "
                  + vector.get("name")
                  + " expected "
                  + Json.stringify(expected.get("value"))
                  + "/"
                  + expected.get("variationIndex")
                  + "/"
                  + describe(reason)
                  + " got "
                  + Json.stringify(got.value())
                  + "/"
                  + got.variationIndex()
                  + "/"
                  + describe(got.reason()));
        }
      }
    }
    assertTrue(vectors > 0, "no vectors were found");
    assertTrue(pins > 0, "no bucket pins were found");
    Path out = Path.of("target/vectors-count.txt");
    Files.createDirectories(out.getParent());
    Files.writeString(out, "vectors: " + vectors + " pins: " + pins + "\n", StandardCharsets.UTF_8);
    assertEquals(List.of(), failures);
  }
}
