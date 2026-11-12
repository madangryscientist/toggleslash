package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;

import java.nio.charset.StandardCharsets;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;

class JsonTest {

  private static String decode(int... bytes) {
    byte[] data = new byte[bytes.length];
    for (int i = 0; i < bytes.length; i++) {
      data[i] = (byte) bytes[i];
    }
    return Json.decodeUtf8(data);
  }

  @Test
  void parsesEveryValueKind() {
    Object value = Json.parse("{\"a\":[1,2.5,-0.0,true,false,null,\"x\"],\"b\":{}}");
    Map<?, ?> map = (Map<?, ?>) value;
    List<?> list = (List<?>) map.get("a");
    assertEquals(1.0, list.get(0));
    assertEquals(2.5, list.get(1));
    assertEquals(Boolean.TRUE, list.get(3));
    assertEquals(null, list.get(5));
    assertEquals("x", list.get(6));
    assertEquals(Map.of(), map.get("b"));
  }

  @Test
  void keepsBigNumbersAsDoubles() {
    assertEquals(9007199254740992.0, Json.parse("9007199254740993"));
    assertEquals(Double.POSITIVE_INFINITY, Json.parse("1e999"));
  }

  @Test
  void lastDuplicateKeyWins() {
    assertEquals(Map.of("a", 2.0), Json.parse("{\"a\":1,\"a\":2}"));
  }

  @Test
  void keepsALoneSurrogateEscape() {
    assertEquals("\ud800", Json.parse("\"\\ud800\""));
    assertEquals("\ud83d\ude00", Json.parse("\"\\ud83d\\ude00\""));
  }

  @Test
  void rejectsWhatIsNotJson() {
    for (String text :
        List.of(
            "", "{", "[1,]", "{\"a\"}", "01", "1.", ".5", "+1", "NaN", "'a'", "[1] x", "\"\n\"")) {
      assertThrows(Json.ParseException.class, () -> Json.parse(text), text);
    }
  }

  @Test
  void rejectsTooDeepNesting() {
    String deep = "[".repeat(5000) + "]".repeat(5000);
    assertThrows(Json.ParseException.class, () -> Json.parse(deep));
  }

  @Test
  void skipsAByteOrderMark() {
    assertEquals(1.0, Json.parse(new byte[] {(byte) 0xEF, (byte) 0xBB, (byte) 0xBF, '1'}));
  }

  @Test
  void replacesInvalidUtf8LikeTheWhatwgDecoder() {
    assertEquals("\ufffd(", decode(0xC3, 0x28));
    assertEquals("\ufffd", decode(0xE2, 0x82));
    assertEquals("\ufffd", decode(0xF0, 0x9F, 0x98));
    assertEquals("\ufffd\ufffd\ufffd", decode(0xED, 0xA0, 0x80));
    assertEquals("\ufffd\ufffd", decode(0xC0, 0x80));
    assertEquals("\ufffd", decode(0xF5));
    assertEquals("a\ufffdb", decode('a', 0xFF, 'b'));
    assertEquals("\u00e9", decode(0xC3, 0xA9));
    assertEquals("é".repeat(3), decode("ééé".getBytes(StandardCharsets.UTF_8)));
  }

  private static String decode(byte[] bytes) {
    return Json.decodeUtf8(bytes);
  }
}
