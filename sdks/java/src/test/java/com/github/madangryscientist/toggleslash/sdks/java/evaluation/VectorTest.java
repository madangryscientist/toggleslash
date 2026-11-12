package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.stream.Stream;
import org.junit.jupiter.api.Test;

/** Runs every shared evaluation vector and every bucket pin, read from the repository. */
class VectorTest {
  private static final Path DIR = Path.of("../../fixtures/evaluation");

  private static Json read(Path file) throws IOException {
    return Json.parse(Files.readAllBytes(file));
  }

  private static Json reasonJson(Reason reason) {
    Map<String, Json> fields = new LinkedHashMap<>();
    fields.put("kind", new Json.Str(reason.kind().name()));
    if (reason.ruleIndex() != null) {
      fields.put("ruleIndex", new Json.Num(reason.ruleIndex()));
    }
    if (reason.flagKey() != null) {
      fields.put("flagKey", new Json.Str(reason.flagKey()));
    }
    if (reason.code() != null) {
      fields.put("code", new Json.Str(reason.code().name()));
    }
    return new Json.Obj(fields);
  }

  private static Json resultJson(Result result) {
    Map<String, Json> fields = new LinkedHashMap<>();
    fields.put("value", result.value());
    fields.put(
        "variationIndex",
        result.variationIndex() == null ? Json.NULL : new Json.Num(result.variationIndex()));
    fields.put("reason", reasonJson(result.reason()));
    return new Json.Obj(fields);
  }

  @Test
  void everyVectorAndPin() throws IOException {
    int vectors = 0;
    int pins = 0;
    List<String> failures = new ArrayList<>();
    List<Path> files;
    try (Stream<Path> list = Files.list(DIR)) {
      files = list.filter(p -> p.toString().endsWith(".json")).sorted().toList();
    }
    for (Path file : files) {
      Json.Obj root = (Json.Obj) read(file);
      String name = file.getFileName().toString();
      if (name.equals("buckets.json")) {
        for (Json item : ((Json.Arr) root.get("buckets")).items()) {
          Json.Obj pin = (Json.Obj) item;
          int bucket =
              Bucketing.bucket(
                  ((Json.Str) pin.get("flagKey")).value(),
                  ((Json.Str) pin.get("salt")).value(),
                  ((Json.Str) pin.get("contextKey")).value());
          pins++;
          double want = ((Json.Num) pin.get("bucket")).value();
          if (bucket != want) {
            failures.add("pin " + pin.toJson() + " got " + bucket);
          }
        }
        continue;
      }
      for (Json item : ((Json.Arr) root.get("vectors")).items()) {
        Json.Obj vector = (Json.Obj) item;
        vectors++;
        Map<String, Json> ruleset = new LinkedHashMap<>();
        ruleset.put("flags", vector.get("flags"));
        ruleset.put("segments", vector.get("segments"));
        Result result =
            Evaluator.evaluate(
                Ruleset.load(new Json.Obj(ruleset)),
                ((Json.Str) vector.get("flagKey")).value(),
                vector.get("context"));
        Json got = resultJson(result);
        Json want = vector.get("expected");
        if (!got.equals(want)) {
          failures.add(
              name
                  + ": "
                  + ((Json.Str) vector.get("name")).value()
                  + "\n  want "
                  + want.toJson()
                  + "\n  got  "
                  + got.toJson());
        }
      }
    }
    assertTrue(vectors > 0 && pins > 0, "read no vectors or no pins");
    Files.createDirectories(Path.of("target"));
    Files.writeString(
        Path.of("target/vectors.txt"),
        "vectors: " + vectors + " pins: " + pins + "\n",
        StandardCharsets.UTF_8);
    assertEquals(List.of(), failures.size() > 20 ? failures.subList(0, 20) : failures);
    assertEquals(0, failures.size());
  }
}
