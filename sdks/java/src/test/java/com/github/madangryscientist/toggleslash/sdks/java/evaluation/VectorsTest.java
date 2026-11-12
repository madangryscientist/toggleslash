package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.stream.Stream;
import org.junit.jupiter.api.Test;

/** Runs every shared evaluation vector and bucket pin, and writes the counts for bin/test. */
class VectorsTest {
  private static final Path DIR = Path.of("..", "..", "fixtures", "evaluation");

  private static Object read(Path file) throws IOException {
    return Json.parse(Files.readString(file, StandardCharsets.UTF_8));
  }

  @SuppressWarnings("unchecked")
  private static Map<String, Object> map(Object value) {
    return (Map<String, Object>) value;
  }

  @SuppressWarnings("unchecked")
  private static List<Object> list(Object value) {
    return (List<Object>) value;
  }

  @Test
  void vectorsAndPins() throws IOException {
    List<Path> files;
    try (Stream<Path> stream = Files.list(DIR)) {
      files = stream.filter(p -> p.toString().endsWith(".json")).sorted().toList();
    }
    int vectors = 0;
    int pins = 0;
    List<String> failures = new ArrayList<>();
    for (Path file : files) {
      Map<String, Object> document = map(read(file));
      if (file.getFileName().toString().equals("buckets.json")) {
        for (Object item : list(document.get("buckets"))) {
          Map<String, Object> pin = map(item);
          String flagKey = (String) pin.get("flagKey");
          String salt = (String) pin.get("salt");
          String contextKey = (String) pin.get("contextKey");
          long uint32 = ((Double) pin.get("uint32")).longValue();
          int bucket = ((Double) pin.get("bucket")).intValue();
          pins++;
          if (Bucket.uint32(flagKey, salt, contextKey) != uint32
              || Bucket.bucket(flagKey, salt, contextKey) != bucket) {
            failures.add("pin " + flagKey + "/" + salt + "/" + contextKey);
          }
        }
        continue;
      }
      for (Object item : list(document.get("vectors"))) {
        Map<String, Object> vector = map(item);
        vectors++;
        Map<String, Object> document2 = new HashMap<>();
        document2.put("flags", vector.get("flags"));
        document2.put("segments", vector.get("segments"));
        Ruleset ruleset = Ruleset.create(document2);
        Result result =
            Evaluator.evaluate(ruleset, (String) vector.get("flagKey"), vector.get("context"));
        Map<String, Object> expected = map(vector.get("expected"));
        Object expectedIndex = expected.get("variationIndex");
        boolean same =
            java.util.Objects.equals(expected.get("value"), result.value())
                && java.util.Objects.equals(
                    expectedIndex,
                    result.variationIndex() == null ? null : result.variationIndex().doubleValue())
                && expected.get("reason").equals(result.reason().toMap());
        if (!same) {
          failures.add(file.getFileName() + ": " + vector.get("name") + " got " + result);
        }
      }
    }
    Files.createDirectories(Path.of("target"));
    Files.writeString(
        Path.of("target", "vectors.txt"), "vectors: " + vectors + " pins: " + pins + "\n");
    assertTrue(vectors > 0 && pins > 0, "read no vectors");
    assertEquals(List.of(), failures);
  }
}
