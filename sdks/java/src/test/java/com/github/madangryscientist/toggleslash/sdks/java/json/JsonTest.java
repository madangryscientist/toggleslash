package com.github.madangryscientist.toggleslash.sdks.java.json;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;

import java.util.Arrays;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;

class JsonTest {
  @Test
  void parsesScalarsAndContainers() {
    assertEquals(
        Map.of("a", List.of(1.0, true, "xé"), "b", Map.of()),
        Json.parse("{\"a\":[1,true,\"x\\u00e9\"],\"b\":{}}"));
    assertEquals(1.5e3, Json.parse("1.5e3"));
    assertEquals(null, Json.parse("null"));
  }

  @Test
  void duplicateKeysKeepTheLast() {
    assertEquals(Map.of("a", 2.0), Json.parse("{\"a\":1,\"a\":2}"));
  }

  @Test
  void rejectsBadDocuments() {
    for (String bad :
        new String[] {"", "{", "[1,]", "{\"a\":1} x", "\"a\nb\"", "01", "\"\\ud800\\"}) {
      assertThrows(JsonException.class, () -> Json.parse(bad), bad);
    }
  }

  @Test
  void deepNestingIsAnErrorNotACrash() {
    String deep = "[".repeat(100000);
    assertThrows(JsonException.class, () -> Json.parse(deep));
    assertEquals(1, ((List<?>) Json.parse("[[[[[[[[[[1]]]]]]]]]]")).size());
    assertEquals(1, ((List<?>) Json.parse("[".repeat(512) + "]".repeat(512))).size());
  }

  @Test
  void writesAscii() {
    assertEquals(
        "{\"a\":[1,null,\"\\u00e9\\ud83d\\ude00\"]}",
        Json.write(Map.of("a", Arrays.asList(1.0, Double.NaN, "é😀"))));
  }
}
