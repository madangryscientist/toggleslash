package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.IOException;
import java.nio.file.Path;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.stream.Stream;
import org.junit.jupiter.api.AfterAll;
import org.junit.jupiter.api.DynamicTest;
import org.junit.jupiter.api.TestFactory;

/** Runs every shared evaluation vector and every bucket pin from fixtures/evaluation. */
class VectorsTest {
  private static final AtomicInteger VECTORS = new AtomicInteger();
  private static final AtomicInteger PINS = new AtomicInteger();

  /** The result as the vectors spell it. */
  static Map<String, Object> toJson(EvalResult result) {
    Map<String, Object> reason = new LinkedHashMap<>();
    Reason r = result.reason();
    reason.put("kind", r.kind().name());
    switch (r.kind()) {
      case ERROR:
        reason.put("code", r.errorCode().name());
        break;
      case RULE_MATCH:
        reason.put("ruleIndex", (double) r.ruleIndex());
        break;
      case PREREQUISITE_FAILED:
        reason.put("flagKey", r.flagKey());
        break;
      default:
        break;
    }
    Map<String, Object> json = new LinkedHashMap<>();
    json.put("value", result.value());
    json.put(
        "variationIndex",
        result.variationIndex() == null ? null : (double) result.variationIndex());
    json.put("reason", reason);
    return json;
  }

  @TestFactory
  Stream<DynamicTest> evaluationVectors() throws IOException {
    List<Path> files = TestFiles.files("fixtures/evaluation");
    assertTrue(!files.isEmpty(), "read no files");
    Stream.Builder<DynamicTest> tests = Stream.builder();
    for (Path file : files) {
      String name = file.getFileName().toString();
      Map<String, Object> document = TestFiles.object(TestFiles.readJson(file));
      if (name.equals("buckets.json")) {
        for (Object pin : TestFiles.array(document.get("buckets"))) {
          Map<String, Object> p = TestFiles.object(pin);
          tests.add(
              DynamicTest.dynamicTest(
                  name + ": " + p.get("flagKey") + "/" + p.get("contextKey"), () -> checkPin(p)));
        }
        continue;
      }
      for (Object vector : TestFiles.array(document.get("vectors"))) {
        Map<String, Object> v = TestFiles.object(vector);
        tests.add(DynamicTest.dynamicTest(name + ": " + v.get("name"), () -> checkVector(name, v)));
      }
    }
    return tests.build();
  }

  private static void checkPin(Map<String, Object> pin) {
    PINS.incrementAndGet();
    String flagKey = (String) pin.get("flagKey");
    String salt = (String) pin.get("salt");
    String contextKey = (String) pin.get("contextKey");
    assertEquals(
        ((Double) pin.get("uint32")).longValue(), Bucketing.uint32(flagKey, salt, contextKey));
    assertEquals(
        ((Double) pin.get("bucket")).intValue(), Bucketing.bucket(flagKey, salt, contextKey));
  }

  private static void checkVector(String file, Map<String, Object> vector) {
    VECTORS.incrementAndGet();
    Map<String, Object> root = new LinkedHashMap<>();
    root.put("flags", vector.get("flags"));
    root.put("segments", vector.get("segments"));
    Ruleset ruleset = Ruleset.load(root);
    EvalResult actual =
        Evaluator.evaluate(ruleset, (String) vector.get("flagKey"), vector.get("context"));
    Map<String, Object> got = toJson(actual);
    Object expected = vector.get("expected");
    assertTrue(
        TestFiles.same(expected, got),
        () ->
            file
                + ": "
                + vector.get("name")
                + ": expected "
                + Json.write(expected)
                + " but got "
                + Json.write(got));
  }

  @AfterAll
  static void report() {
    System.out.println("vectors: " + VECTORS.get() + " pins: " + PINS.get());
    assertTrue(VECTORS.get() > 0 && PINS.get() > 0, "read no vectors or no pins");
  }
}
