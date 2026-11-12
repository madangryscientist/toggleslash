package com.github.madangryscientist.toggleslash.sdks.java.testservice;

import com.github.madangryscientist.toggleslash.sdks.java.ClientOptions;
import com.github.madangryscientist.toggleslash.sdks.java.Evaluation;
import com.github.madangryscientist.toggleslash.sdks.java.EvaluationEvent;
import com.github.madangryscientist.toggleslash.sdks.java.Status;
import com.github.madangryscientist.toggleslash.sdks.java.ToggleSlashClient;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Json;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;
import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.OptionalInt;
import java.util.OptionalLong;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.Executors;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * The Java SDK's conformance test service: an HTTP server that wraps {@link ToggleSlashClient} in
 * the protocol of {@code docs/sdk-test-service.md}. It catches nothing the SDK throws: a call that
 * raises answers 500 with the error text. The accessor map is in this directory's README.
 */
public final class Main {

  /** A client and what the service recorded around it. */
  private static final class Entry {
    private final List<Map<String, Object>> hookEvents = new CopyOnWriteArrayList<>();
    private final List<String> errors = new CopyOnWriteArrayList<>();
    private ToggleSlashClient client;
  }

  private static final Map<String, Entry> CLIENTS = new ConcurrentHashMap<>();
  private static final AtomicInteger CREATED = new AtomicInteger();
  private static final Pattern CLIENT_ROUTE = Pattern.compile("^/clients/([^/]+)/([a-z-]+)$");

  private Main() {}

  /** Starts the service on a port the OS picks and prints where it listens. */
  public static void main(String[] args) throws IOException {
    HttpServer server =
        HttpServer.create(new InetSocketAddress(InetAddress.getByName("127.0.0.1"), 0), 0);
    server.setExecutor(Executors.newCachedThreadPool());
    server.createContext("/", Main::handle);
    server.start();
    System.out.println("listening http://127.0.0.1:" + server.getAddress().getPort());
    System.out.flush();
  }

  private static void handle(HttpExchange exchange) throws IOException {
    int status;
    String body;
    try {
      Object[] answer = route(exchange);
      status = (Integer) answer[0];
      body = (String) answer[1];
    } catch (Throwable e) {
      status = 500;
      body = e + "\n" + stack(e);
    }
    byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
    exchange.getResponseHeaders().add("Content-Type", "application/json");
    exchange.sendResponseHeaders(status, bytes.length == 0 ? -1 : bytes.length);
    if (bytes.length > 0) {
      exchange.getResponseBody().write(bytes);
    }
    exchange.close();
  }

  private static String stack(Throwable e) {
    StringBuilder out = new StringBuilder();
    for (StackTraceElement frame : e.getStackTrace()) {
      out.append("  at ").append(frame).append('\n');
    }
    return out.toString();
  }

  private static Object[] ok(Object json) {
    return new Object[] {200, Out.write(json)};
  }

  private static Object[] route(HttpExchange exchange) throws IOException {
    String method = exchange.getRequestMethod();
    String path = exchange.getRequestURI().getPath();
    byte[] raw = exchange.getRequestBody().readAllBytes();
    if (method.equals("GET") && path.equals("/")) {
      return ok(
          Map.of("name", "toggleslash-sdk (Java)", "capabilities", List.of("integer-accessor")));
    }
    if (method.equals("POST") && path.equals("/clients")) {
      return create(object(raw));
    }
    Matcher match = CLIENT_ROUTE.matcher(path);
    Entry entry = match.matches() ? CLIENTS.get(match.group(1)) : null;
    if (entry == null) {
      return new Object[] {404, "no such client"};
    }
    String key = method + " " + match.group(2);
    switch (key) {
      case "POST ready":
        return ok(Map.of("ready", entry.client.ready()));
      case "POST evaluate":
        return ok(evaluate(entry, object(raw)));
      case "GET status":
        return ok(status(entry.client.status()));
      case "GET hook-events":
        return ok(Map.of("events", entry.hookEvents));
      case "GET errors":
        return ok(Map.of("count", entry.errors.size(), "messages", entry.errors));
      case "POST close":
        entry.client.close();
        return ok(Map.of());
      default:
        return new Object[] {404, "no such route"};
    }
  }

  @SuppressWarnings("unchecked")
  private static Map<String, Object> object(byte[] raw) {
    if (raw.length == 0) {
      return Map.of();
    }
    Object parsed = Json.parse(raw);
    if (!(parsed instanceof Map)) {
      throw new IllegalArgumentException("the body is not a JSON object");
    }
    return (Map<String, Object>) parsed;
  }

  private static Duration millis(Map<String, Object> body, String name) {
    return body.get(name) instanceof Number n ? Duration.ofMillis(n.longValue()) : null;
  }

  private static Object[] create(Map<String, Object> body) {
    Entry entry = new Entry();
    boolean onErrorThrows = Boolean.TRUE.equals(body.get("onErrorThrows"));
    boolean hookThrows = Boolean.TRUE.equals(body.get("hookThrows"));
    ClientOptions.Builder options =
        ClientOptions.builder(String.valueOf(body.get("url")), String.valueOf(body.get("sdkKey")))
            .onError(error -> {
              entry.errors.add(String.valueOf(error.getMessage()));
              if (onErrorThrows) {
                throw new IllegalStateException("onError threw, as the test asked");
              }
            });
    if (body.get("stream") instanceof Boolean stream) {
      options.stream(stream);
    }
    Duration poll = millis(body, "pollIntervalMs");
    if (poll != null) {
      options.pollInterval(poll);
    }
    Duration flush = millis(body, "flushIntervalMs");
    if (flush != null) {
      options.flushInterval(flush);
    }
    Duration timeout = millis(body, "requestTimeoutMs");
    if (timeout != null) {
      options.requestTimeout(timeout);
    }
    if (Boolean.TRUE.equals(body.get("hook")) || hookThrows) {
      options.afterEvaluation(event -> {
        entry.hookEvents.add(hookEvent(event));
        if (hookThrows) {
          throw new IllegalStateException("the hook threw, as the test asked");
        }
      });
    }
    entry.client = ToggleSlashClient.create(options.build());
    String id = String.valueOf(CREATED.incrementAndGet());
    CLIENTS.put(id, entry);
    return new Object[] {201, Out.write(Map.of("id", id))};
  }

  private static Object evaluate(Entry entry, Map<String, Object> body) {
    String flag = String.valueOf(body.get("flagKey"));
    Object context = body.get("context");
    Object fallback = body.get("fallback");
    String accessor = String.valueOf(body.get("accessor"));
    ToggleSlashClient client = entry.client;
    Evaluation<?> detail;
    switch (accessor) {
      case "bool":
        detail = client.boolVariationDetail(flag, context, (Boolean) fallback);
        break;
      case "string":
        detail = client.stringVariationDetail(flag, context, (String) fallback);
        break;
      case "number":
        detail = client.doubleVariationDetail(flag, context, ((Number) fallback).doubleValue());
        break;
      case "int":
        detail = client.intVariationDetail(flag, context, ((Number) fallback).longValue());
        break;
      default:
        detail = client.jsonVariationDetail(flag, context, fallback);
        break;
    }
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("value", detail.value());
    out.put("variationIndex", index(detail.variationIndex()));
    out.put("reason", reason(detail.reason()));
    return out;
  }

  private static Object index(OptionalInt value) {
    return value.isPresent() ? (Object) value.getAsInt() : null;
  }

  private static Map<String, Object> reason(Reason reason) {
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("kind", reason.kind().name());
    if (reason.ruleIndex().isPresent()) {
      out.put("ruleIndex", reason.ruleIndex().getAsInt());
    }
    if (reason.flagKey() != null) {
      out.put("flagKey", reason.flagKey());
    }
    if (reason.errorCode() != null) {
      out.put("code", reason.errorCode().name());
    }
    return out;
  }

  private static Map<String, Object> hookEvent(EvaluationEvent event) {
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("flagKey", event.flagKey());
    out.put("context", event.context());
    out.put("value", event.value());
    out.put("variationIndex", index(event.variationIndex()));
    out.put("reason", reason(event.reason()));
    out.put("etag", event.etag().orElse(null));
    OptionalLong version = event.flagVersion();
    out.put("flagVersion", version.isPresent() ? (Object) version.getAsLong() : null);
    return out;
  }

  private static Map<String, Object> status(Status status) {
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("etag", status.etag().orElse(null));
    Optional<java.time.Instant> at = status.lastSyncAt();
    out.put("lastSyncAt", at.isPresent() ? (Object) at.get().toEpochMilli() : null);
    out.put("stream", status.stream().wire());
    out.put("lastError", status.lastError().orElse(null));
    return out;
  }

  /** A small JSON writer for the service's answers. */
  static final class Out {
    private Out() {}

    static String write(Object value) {
      StringBuilder out = new StringBuilder();
      write(value, out);
      return out.toString();
    }

    private static void write(Object value, StringBuilder out) {
      if (value == null) {
        out.append("null");
      } else if (value instanceof Boolean || value instanceof Integer || value instanceof Long) {
        out.append(value);
      } else if (value instanceof Number n) {
        double d = n.doubleValue();
        if (Double.isNaN(d) || Double.isInfinite(d)) {
          out.append("null");
        } else if (d == Math.rint(d) && Math.abs(d) < 1e15) {
          out.append((long) d);
        } else {
          out.append(d);
        }
      } else if (value instanceof String s) {
        string(s, out);
      } else if (value instanceof Map<?, ?> map) {
        out.append('{');
        boolean first = true;
        for (Map.Entry<?, ?> e : map.entrySet()) {
          if (!first) {
            out.append(',');
          }
          first = false;
          string(String.valueOf(e.getKey()), out);
          out.append(':');
          write(e.getValue(), out);
        }
        out.append('}');
      } else if (value instanceof List<?> list) {
        out.append('[');
        for (int i = 0; i < list.size(); i++) {
          if (i > 0) {
            out.append(',');
          }
          write(list.get(i), out);
        }
        out.append(']');
      } else {
        string(String.valueOf(value), out);
      }
    }

    private static void string(String s, StringBuilder out) {
      out.append('"');
      for (int i = 0; i < s.length(); i++) {
        char c = s.charAt(i);
        if (c == '"' || c == '\\') {
          out.append('\\').append(c);
        } else if (c < 0x20 || Character.isSurrogate(c)) {
          boolean pair =
              Character.isHighSurrogate(c)
                  && i + 1 < s.length()
                  && Character.isLowSurrogate(s.charAt(i + 1));
          if (pair) {
            out.append(c).append(s.charAt(++i));
          } else {
            out.append(String.format("\\u%04x", (int) c));
          }
        } else {
          out.append(c);
        }
      }
      out.append('"');
    }
  }
}
