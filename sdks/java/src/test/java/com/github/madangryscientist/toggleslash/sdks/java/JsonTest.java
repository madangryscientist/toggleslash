package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;

import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;

/** The strict parser and the ASCII writer. */
class JsonTest {
  @Test
  void parsesEveryKind() throws Exception {
    Object tree = Json.parse(" {\"a\":[1,2.5,-0,true,false,null,\"x\"],\"b\":{}} ");
    Map<?, ?> map = (Map<?, ?>) tree;
    List<?> list = (List<?>) map.get("a");
    assertEquals(1.0, list.get(0));
    assertEquals(2.5, list.get(1));
    assertEquals(Boolean.TRUE, list.get(3));
    assertEquals(null, list.get(5));
    assertEquals("x", list.get(6));
    assertEquals(Map.of(), map.get("b"));
  }

  @Test
  void aDuplicateKeyKeepsTheLast() throws Exception {
    assertEquals(2.0, ((Map<?, ?>) Json.parse("{\"a\":1,\"a\":2}")).get("a"));
  }

  @Test
  void rejectsWhatIsNotJson() {
    String[] bad = {
      "", "{", "[1,]", "{\"a\"}", "01", "1.", "\"a", "tru", "[1] x", "{'a':1}", "NaN", "\"\\x\"",
      "\"\t\""
    };
    for (String text : bad) {
      assertThrows(JsonException.class, () -> Json.parse(text), text);
    }
  }

  @Test
  void nestingHasALimit() throws Exception {
    Json.parse("[".repeat(Json.MAX_DEPTH) + "]".repeat(Json.MAX_DEPTH));
    String deep = "[".repeat(Json.MAX_DEPTH + 1) + "]".repeat(Json.MAX_DEPTH + 1);
    assertThrows(JsonException.class, () -> Json.parse(deep));
  }

  @Test
  void keepsALoneSurrogateEscape() throws Exception {
    assertEquals("\ud800", Json.parse("\"\\ud800\""));
    assertEquals("\ud83d\ude00", Json.parse("\"\\ud83d\\ude00\""));
  }

  @Test
  void writesAscii() {
    assertEquals("\"\\u00e9\\ud83d\\ude00\\\"\\\\\"", Json.stringify("\u00e9\ud83d\ude00\"\\"));
    assertEquals(
        "[-0,0,1,2.5,null,null]",
        Json.stringify(List.of(-0.0, 0.0, 1.0, 2.5, Double.NaN, Double.POSITIVE_INFINITY)));
  }
}
