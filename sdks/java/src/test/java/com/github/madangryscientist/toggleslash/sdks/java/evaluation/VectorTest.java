package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.github.madangryscientist.toggleslash.sdks.java.Reason;
import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.stream.Stream;
import org.junit.jupiter.api.Test;

/**
 * Runs every shared evaluation vector and every bucket pin in {@code fixtures/evaluation}, the two
 * {@code load:} vectors included, and writes the counts to {@code target/vectors.txt} for {@code
 * bin/test} to print.
 */
final class VectorTest {
  private static final Path FIXTURES = Path.of("..", "..", "fixtures", "evaluation");

  @SuppressWarnings("unchecked")
  private static Map<String, Object> object(Object value) {
    return (Map<String, Object>) value;
  }

  @SuppressWarnings("unchecked")
  private static List<Object> array(Object value) {
    return (List<Object>) value;
  }

  private static Object read(Path file) throws IOException {
    return Json.parse(Files.readString(file, StandardCharsets.UTF_8));
  }

  /** JSON equality: numbers compare by value, so {@code 1} and {@code 1.0} are one number. */
  private static boolean same(Object a, Object b) {
    if (a == null || b == null) {
      return a == b;
    }
    if (a instanceof Number x && b instanceof Number y) {
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
      for (Map.Entry<?, ?> e : x.entrySet()) {
        if (!same(e.getValue(), y.get(e.getKey()))) {
          return false;
        }
      }
      return true;
    }
    return a.equals(b);
  }

  private static String describe(Result result) {
    Reason r = result.reason();
    return Json.write(result.value())
        + " index="
        + result.variationIndex()
        + " "
        + r.kind()
        + " rule="
        + r.ruleIndex()
        + " flag="
        + r.flagKey()
        + " code="
        + r.code();
  }

  private static boolean matches(Result actual, Map<String, Object> expected) {
    Map<String, Object> reason = object(expected.get("reason"));
    Object index = expected.get("variationIndex");
    Reason r = actual.reason();
    return same(actual.value(), expected.get("value"))
        && same(actual.variationIndex(), index)
        && r.kind().name().equals(reason.get("kind"))
        && same(r.ruleIndex(), reason.get("ruleIndex"))
        && same(r.flagKey(), reason.get("flagKey"))
        && same(r.code() == null ? null : r.code().name(), reason.get("code"));
  }

  @Test
  void everyVectorAndEveryBucketPinPasses() throws IOException {
    List<String> failures = new ArrayList<>();
    int vectors = 0;
    int pins = 0;
    List<Path> files;
    try (Stream<Path> listing = Files.list(FIXTURES)) {
      files = listing.filter(p -> p.toString().endsWith(".json")).sorted().toList();
    }
    for (Path file : files) {
      Map<String, Object> parsed = object(read(file));
      if (file.getFileName().toString().equals("buckets.json")) {
        for (Object item : array(parsed.get("buckets"))) {
          Map<String, Object> pin = object(item);
          int bucket =
              Bucket.of(
                  (String) pin.get("flagKey"),
                  (String) pin.get("salt"),
                  (String) pin.get("contextKey"));
          pins++;
          if (bucket != ((Number) pin.get("bucket")).intValue()) {
            failures.add("bucket " + pin + " gave " + bucket);
          }
        }
        continue;
      }
      for (Object item : array(parsed.get("vectors"))) {
        Map<String, Object> vector = object(item);
        vectors++;
        Ruleset ruleset =
            Ruleset.of(
                Map.of(
                    "flags", vector.get("flags") == null ? List.of() : vector.get("flags"),
                    "segments",
                        vector.get("segments") == null ? List.of() : vector.get("segments")));
        Result actual =
            Evaluator.evaluate(ruleset, (String) vector.get("flagKey"), vector.get("context"));
        Map<String, Object> expected = object(vector.get("expected"));
        if (!matches(actual, expected)) {
          failures.add(
              file.getFileName()
                  + ": "
                  + vector.get("name")
                  + ": got "
                  + describe(actual)
                  + ", expected "
                  + Json.write(expected));
        }
      }
    }
    Files.createDirectories(Path.of("target"));
    Files.writeString(
        Path.of("target", "vectors.txt"), "vectors: " + vectors + " pins: " + pins + "\n");
    assertTrue(vectors > 0 && pins > 0, "no vectors were found under " + FIXTURES.toAbsolutePath());
    assertEquals(List.of(), failures, failures.size() + " vectors failed");
  }
}
