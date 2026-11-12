package com.github.madangryscientist.toggleslash.sdks.java.json;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;

class JsonTest {
  @Test
  void parsesEveryValueKind() {
    Map<?, ?> map = (Map<?, ?>) Json.parse("{\"a\":[1,2.5,\"x\",true,false,null],\"b\":{}}");
    assertEquals(List.of(1.0, 2.5, "x", true, false), ((List<?>) map.get("a")).subList(0, 5));
    assertEquals(null, ((List<?>) map.get("a")).get(5));
    assertEquals(Map.of(), map.get("b"));
  }

  @Test
  void rejectsWhatJsonRejects() {
    for (String bad :
        new String[] {
          "",
          "{",
          "[1,]",
          "{\"a\":1,}",
          "01",
          "1.",
          ".5",
          "+1",
          "'a'",
          "nul",
          "[1] x",
          "\"\n\"",
          "\"\\x\"",
          "NaN"
        }) {
      assertThrows(JsonException.class, () -> Json.parse(bad), bad);
    }
  }

  @Test
  void theLastDuplicateKeyWins() {
    assertEquals(2.0, ((Map<?, ?>) Json.parse("{\"a\":1,\"a\":2}")).get("a"));
  }

  @Test
  void anOverflowingNumberIsInfinityAndNegativeZeroIsKept() {
    assertEquals(Double.POSITIVE_INFINITY, Json.parse("1e999"));
    assertEquals(Double.doubleToLongBits(-0.0), Double.doubleToLongBits((Double) Json.parse("-0")));
  }

  @Test
  void aLoneSurrogateEscapeIsKept() {
    assertEquals("\ud800", Json.parse("\"\\ud800\""));
    assertEquals("\ud83d\ude00", Json.parse("\"\\ud83d\\ude00\""));
  }

  @Test
  void deepNestingIsRefusedNotACrash() {
    String deep = "[".repeat(5000) + "]".repeat(5000);
    assertThrows(JsonException.class, () -> Json.parse(deep));
    String ok = "[".repeat(500) + "]".repeat(500);
    assertTrue(Json.parse(ok) instanceof List<?>);
  }

  @Test
  void writesWhatItParses() {
    String text = "{\"a\":[1,2.5,\"x\\n\\u0001\",true,null],\"b\":\"\\ud800\"}";
    assertEquals(text, Json.write(Json.parse(text)));
  }

  @Test
  void writesAWholeDoubleAsAnInteger() {
    assertEquals("[3,-2,0.5]", Json.write(List.of(3.0, -2.0, 0.5)));
  }

  @Test
  void aNonFiniteNumberCannotBeWritten() {
    assertThrows(JsonException.class, () -> Json.write(Double.NaN));
    assertThrows(JsonException.class, () -> Json.write(Double.POSITIVE_INFINITY));
  }
}
