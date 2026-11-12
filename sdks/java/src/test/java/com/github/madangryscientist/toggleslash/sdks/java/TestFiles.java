package com.github.madangryscientist.toggleslash.sdks.java;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;

/** Helpers that read the repository's fixtures from the working directory the tests run in. */
final class TestFiles {
  private TestFiles() {}

  /** The repository root: the SDK is at {@code sdks/java}, and Maven runs in it. */
  static Path repository() {
    return Path.of("").toAbsolutePath().resolve("../..").normalize();
  }

  static Object readJson(Path path) throws IOException {
    return Json.parse(Utf8.decode(Files.readAllBytes(path)));
  }

  static List<Path> files(String directory) throws IOException {
    List<Path> files = new ArrayList<>();
    try (var stream = Files.list(repository().resolve(directory))) {
      stream.filter(p -> p.toString().endsWith(".json")).sorted().forEach(files::add);
    }
    return files;
  }

  @SuppressWarnings("unchecked")
  static Map<String, Object> object(Object value) {
    return (Map<String, Object>) value;
  }

  @SuppressWarnings("unchecked")
  static List<Object> array(Object value) {
    return (List<Object>) value;
  }

  /**
   * Deep equality by the spec: numbers by IEEE-754 so -0 equals 0, objects by key, arrays by order.
   */
  static boolean same(Object a, Object b) {
    if (a == null || b == null) {
      return a == b;
    }
    if (a instanceof Double && b instanceof Double) {
      return ((Double) a).doubleValue() == ((Double) b).doubleValue();
    }
    if (a instanceof List && b instanceof List) {
      List<?> x = (List<?>) a;
      List<?> y = (List<?>) b;
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
    if (a instanceof Map && b instanceof Map) {
      Map<?, ?> x = (Map<?, ?>) a;
      Map<?, ?> y = (Map<?, ?>) b;
      if (!x.keySet().equals(y.keySet())) {
        return false;
      }
      for (Map.Entry<?, ?> entry : x.entrySet()) {
        if (!same(entry.getValue(), y.get(entry.getKey()))) {
          return false;
        }
      }
      return true;
    }
    return a.equals(b);
  }
}
