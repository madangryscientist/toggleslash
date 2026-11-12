package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertInstanceOf;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.ErrorCode;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;
import java.math.BigDecimal;
import java.math.BigInteger;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;

/** The typed and generic accessors, and the types they hand back. */
@Timeout(60)
final class AccessorsTest {
  private static final Reason MISMATCH = Reason.error(ErrorCode.TYPE_MISMATCH);

  private FakeServer server;
  private Client client;
  private final List<EvaluationEvent> events = Collections.synchronizedList(new ArrayList<>());

  @BeforeEach
  void start() {
    server = new FakeServer();
    server.ruleset =
        FakeServer.serving(
            "\"v1\"",
            Support.ruleset(
                Support.flag("two", "[2.0]", 0, "[]", 1),
                Support.flag("frac", "[2.5]", 0, "[]", 1),
                Support.flag("huge", "[1e19]", 0, "[]", 1),
                Support.flag("max", "[9223372036854775807]", 0, "[]", 1),
                Support.flag("min", "[-9223372036854775808]", 0, "[]", 1),
                Support.flag("big", "[3000000000]", 0, "[]", 1),
                Support.flag("text", "[\"x\"]", 0, "[]", 1),
                Support.flag("bool", "[true]", 0, "[]", 1),
                Support.flag("obj", "[{\"a\":1}]", 0, "[]", 1),
                Support.flag("arr", "[[1,2]]", 0, "[]", 1),
                Support.flag("nul", "[null]", 0, "[]", 1),
                Support.flag(
                    "five",
                    "[false,true]",
                    0,
                    "[{\"clauses\":[{\"attribute\":\"n\",\"op\":\"in\",\"values\":[5]}],\"variation\":1}]",
                    1),
                Support.flag(
                    "half",
                    "[false,true]",
                    0,
                    "[{\"clauses\":[{\"attribute\":\"n\",\"op\":\"in\",\"values\":[2.5]}],\"variation\":1}]",
                    1)));
    client =
        Client.create(Support.options(server).stream(false).afterEvaluation(events::add).build());
    assertTrue(client.ready());
  }

  @AfterEach
  void stop() {
    client.close();
    server.close();
  }

  private static final Map<String, Object> CTX = Map.of("kind", "user", "key", "u1");

  @Test
  void aServedHugeNumberIsATypeMismatchOnTheIntegerAccessor() {
    for (String flag : new String[] {"huge", "max", "frac"}) {
      Detail<Long> detail = client.longVariationDetail(flag, CTX, -1L);
      assertEquals(-1L, detail.value(), flag);
      assertNull(detail.variationIndex(), flag);
      assertEquals(MISMATCH, detail.reason(), flag);
    }
  }

  @Test
  void aWholeNumberWithinSigned64BitsIsServed() {
    assertEquals(2L, client.longVariation("two", CTX, -1L));
    assertEquals(Long.MIN_VALUE, client.longVariation("min", CTX, -1L));
    assertEquals(3_000_000_000L, client.longVariation("big", CTX, -1L));
  }

  @Test
  void aNullFallbackStringAccessorGivenAnotherKindIsATypeMismatch() {
    for (String flag : new String[] {"two", "bool", "obj", "arr", "nul"}) {
      Detail<String> detail = client.stringVariationDetail(flag, CTX, null);
      assertNull(detail.value(), flag);
      assertNull(detail.variationIndex(), flag);
      assertEquals(MISMATCH, detail.reason(), flag);
    }
    assertEquals("x", client.stringVariation("text", CTX, null));
  }

  @Test
  void aGenericAccessorWithAnIntegerFallbackGivesThatType() {
    assertEquals(
        Integer.valueOf(2), assertInstanceOf(Integer.class, client.variation("two", CTX, 7)));
    assertEquals(Long.valueOf(2), assertInstanceOf(Long.class, client.variation("two", CTX, 7L)));
    assertEquals(
        Short.valueOf((short) 2),
        assertInstanceOf(Short.class, client.variation("two", CTX, (short) 7)));
    assertEquals(
        Byte.valueOf((byte) 2),
        assertInstanceOf(Byte.class, client.variation("two", CTX, (byte) 7)));
    assertEquals(2.0, assertInstanceOf(Double.class, client.variation("two", CTX, 7.0)));
    assertEquals(2.0f, assertInstanceOf(Float.class, client.variation("two", CTX, 7.0f)));
  }

  @Test
  void aGenericAccessorWithAnIntegerFallbackMismatchesAFractionOrAValueOutOfRange() {
    Detail<Integer> fraction = client.variationDetail("frac", CTX, 7);
    assertEquals(7, fraction.value());
    assertEquals(MISMATCH, fraction.reason());
    Detail<Integer> big = client.variationDetail("big", CTX, 7);
    assertEquals(7, big.value());
    assertEquals(MISMATCH, big.reason());
    assertEquals(3_000_000_000L, client.variation("big", CTX, 7L));
    Detail<Short> shortBig = client.variationDetail("big", CTX, (short) 7);
    assertEquals(MISMATCH, shortBig.reason());
    Detail<Byte> byteBig = client.variationDetail("two", CTX, (byte) 7);
    assertEquals(Byte.valueOf((byte) 2), byteBig.value());
    Detail<Integer> text = client.variationDetail("text", CTX, 7);
    assertEquals(MISMATCH, text.reason());
    Detail<Long> huge = client.variationDetail("huge", CTX, 7L);
    assertEquals(MISMATCH, huge.reason());
  }

  @Test
  void anUnknownFallbackClassIsATypeMismatch() {
    Detail<StringBuilder> detail = client.variationDetail("text", CTX, new StringBuilder("f"));
    assertEquals(MISMATCH, detail.reason());
    assertEquals("f", detail.value().toString());
  }

  @Test
  void theJsonAccessorComparesKindsAndANullFallbackAcceptsAny() {
    assertEquals(Map.of("a", 1.0), client.jsonVariation("obj", CTX, null));
    assertEquals(List.of(1.0, 2.0), client.jsonVariation("arr", CTX, null));
    assertEquals(Map.of("a", 1.0), client.jsonVariation("obj", CTX, Map.of("z", 9)));
    assertEquals(MISMATCH, client.jsonVariationDetail("arr", CTX, Map.of()).reason());
    assertEquals(MISMATCH, client.jsonVariationDetail("obj", CTX, List.of()).reason());
    assertEquals(MISMATCH, client.jsonVariationDetail("nul", CTX, "f").reason());
    Detail<Object> served = client.jsonVariationDetail("nul", CTX, null);
    assertNull(served.value());
    assertEquals(0, served.variationIndex());
  }

  @Test
  void theHookEventValueIsTheBoxedValueTheCallerReceives() {
    client.longVariation("two", CTX, -1L);
    client.doubleVariation("two", CTX, -1.0);
    client.stringVariation("text", CTX, "f");
    client.booleanVariation("bool", CTX, false);
    client.jsonVariation("obj", CTX, null);
    client.variation("two", CTX, 7);
    assertEquals(6, events.size());
    assertInstanceOf(Long.class, events.get(0).value());
    assertEquals(2L, events.get(0).value());
    assertInstanceOf(Double.class, events.get(1).value());
    assertInstanceOf(String.class, events.get(2).value());
    assertInstanceOf(Boolean.class, events.get(3).value());
    assertInstanceOf(Map.class, events.get(4).value());
    assertInstanceOf(Integer.class, events.get(5).value());
  }

  @Test
  void aContextAttributeOfEachNumericTypeMatchesAsTheJsonNumberOfItsValue() {
    Object[] fives = {
      (byte) 5,
      (short) 5,
      5,
      5L,
      5.0f,
      5.0,
      BigInteger.valueOf(5),
      new BigDecimal("5"),
      new BigDecimal("5.00"),
      new AtomicInteger(5),
      new AtomicLong(5)
    };
    for (Object five : fives) {
      String what = five.getClass().getSimpleName();
      assertTrue(client.booleanVariation("five", Context.user("u").with("n", five), false), what);
      Map<String, Object> raw = Map.of("kind", "user", "key", "u", "attributes", Map.of("n", five));
      assertTrue(client.booleanVariation("five", raw, false), what + " in a map");
    }
    assertTrue(client.booleanVariation("half", Context.user("u").with("n", 2.5f), false));
    assertTrue(client.booleanVariation("half", Context.user("u").with("n", 2.5), false));
    assertTrue(
        client.booleanVariation("half", Context.user("u").with("n", new BigDecimal("2.5")), false));
    assertFalse(client.booleanVariation("five", Context.user("u").with("n", 6), false));
    assertFalse(client.booleanVariation("five", Context.user("u").with("n", "5"), false));
  }
}
