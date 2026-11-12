package com.github.madangryscientist.toggleslash.sdks.java.json;

import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * A JSON value. Numbers are binary64, so {@code 1e999} is infinity and an integer above 2^53 loses
 * precision, as it does in the other implementations. Parsing is {@link #parse(String)}.
 */
public sealed interface Json permits Json.Null, Json.Bool, Json.Num, Json.Str, Json.Arr, Json.Obj {

  /** The JSON {@code null}. */
  Json NULL = new Null();

  /** The JSON {@code true}. */
  Json TRUE = new Bool(true);

  /** The JSON {@code false}. */
  Json FALSE = new Bool(false);

  /** The JSON {@code null}. */
  record Null() implements Json {}

  /**
   * A boolean.
   *
   * @param value the value
   */
  record Bool(boolean value) implements Json {}

  /**
   * A number.
   *
   * @param value the value, as binary64
   */
  record Num(double value) implements Json {
    /** Two numbers are equal when they are equal as numbers, so 0 and -0 are. */
    @Override
    public boolean equals(Object other) {
      return other instanceof Num n && n.value == value;
    }

    @Override
    public int hashCode() {
      return Double.hashCode(value == 0 ? 0.0 : value);
    }
  }

  /**
   * A string.
   *
   * @param value the value; it may hold a lone surrogate
   */
  record Str(String value) implements Json {}

  /**
   * An array.
   *
   * @param items the items, read only
   */
  record Arr(List<Json> items) implements Json {
    /**
     * Creates an array.
     *
     * @param items the items; the list is copied
     */
    public Arr {
      items = Collections.unmodifiableList(new ArrayList<>(items));
    }
  }

  /**
   * An object. A duplicate name keeps the last value.
   *
   * @param fields the fields in order, read only
   */
  record Obj(Map<String, Json> fields) implements Json {
    /**
     * Creates an object.
     *
     * @param fields the fields; the map is copied
     */
    public Obj {
      fields = Collections.unmodifiableMap(new LinkedHashMap<>(fields));
    }

    /**
     * Reads a field.
     *
     * @param name the name
     * @return the value, or null when the field is absent
     */
    public Json get(String name) {
      return fields.get(name);
    }
  }

  /**
   * Parses JSON text.
   *
   * @param text the text
   * @return the value
   * @throws JsonException when the text is not JSON, or nests deeper than 1000
   */
  static Json parse(String text) {
    return JsonParser.parse(text);
  }

  /**
   * Parses UTF-8 bytes: a leading byte order mark is skipped and a bad sequence becomes U+FFFD.
   *
   * @param bytes the bytes
   * @return the value
   * @throws JsonException when the text is not JSON, or nests deeper than 1000
   */
  static Json parse(byte[] bytes) {
    Utf8.Decoder decoder = new Utf8.Decoder(true);
    StringBuilder out = new StringBuilder(bytes.length);
    decoder.decode(bytes, 0, bytes.length, true, out);
    return JsonParser.parse(out.toString());
  }

  /**
   * Builds a value from plain Java objects: null, Boolean, Number, String, List and Map.
   *
   * @param value the object
   * @return the JSON value
   * @throws IllegalArgumentException for any other class
   */
  static Json of(Object value) {
    if (value == null || value instanceof Null) {
      return NULL;
    }
    if (value instanceof Json json) {
      return json;
    }
    if (value instanceof Boolean b) {
      return b ? TRUE : FALSE;
    }
    if (value instanceof Number n) {
      return new Num(n.doubleValue());
    }
    if (value instanceof CharSequence s) {
      return new Str(s.toString());
    }
    if (value instanceof List<?> list) {
      List<Json> items = new ArrayList<>(list.size());
      for (Object item : list) {
        items.add(of(item));
      }
      return new Arr(items);
    }
    if (value instanceof Map<?, ?> map) {
      Map<String, Json> fields = new LinkedHashMap<>();
      for (Map.Entry<?, ?> entry : map.entrySet()) {
        fields.put(String.valueOf(entry.getKey()), of(entry.getValue()));
      }
      return new Obj(fields);
    }
    throw new IllegalArgumentException("not a JSON value: " + value.getClass().getName());
  }

  /**
   * Writes the value as JSON text. Infinity is written as {@code 1e999} and a lone surrogate as an
   * escape, so the text is always valid JSON and reads back to the same value.
   *
   * @return the text
   */
  default String toJson() {
    StringBuilder out = new StringBuilder();
    JsonWriter.write(this, out);
    return out.toString();
  }
}
