package com.github.madangryscientist.toggleslash.sdks.java.testservice;

import com.github.madangryscientist.toggleslash.sdks.java.AfterEvaluation;
import com.github.madangryscientist.toggleslash.sdks.java.Client;
import com.github.madangryscientist.toggleslash.sdks.java.Context;
import com.github.madangryscientist.toggleslash.sdks.java.Detail;
import com.github.madangryscientist.toggleslash.sdks.java.Options;
import com.github.madangryscientist.toggleslash.sdks.java.Status;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;
import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.io.PrintWriter;
import java.io.StringWriter;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.ArrayList;
import java.util.Collections;
import java.util.IdentityHashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.Executors;
import java.util.concurrent.atomic.AtomicInteger;

/**
 * The Java SDK's conformance test service: an HTTP server that wraps {@link Client} so the harness
 * in {@code packages/sdk-conformance} can drive it from outside. The protocol is {@code
 * docs/sdk-test-service.md}; the accessor mapping is in this directory's README. It catches nothing
 * the SDK throws: a call that raises answers 500 with the error text, and the harness fails the
 * scenario.
 */
public final class Main {
  private static final Map<String, Entry> CLIENTS = new ConcurrentHashMap<>();
  private static final AtomicInteger CREATED = new AtomicInteger();

  /** One client with what the service records about it. */
  private record Entry(
      Client client,
      List<Map<String, Object>> hookEvents,
      List<String> errors,
      Map<Context, Object> contexts) {}

  /** A request the service answers with a status other than 200. */
  private static final class Refusal extends RuntimeException {
    private static final long serialVersionUID = 1L;
    private final int status;

    Refusal(int status, String message) {
      super(message);
      this.status = status;
    }
  }

  private Main() {}

  /**
   * Starts the service and prints the line the harness waits for.
   *
   * @param args ignored
   * @throws IOException if the port cannot be opened
   */
  public static void main(String[] args) throws IOException {
    HttpServer server =
        HttpServer.create(new InetSocketAddress(InetAddress.getByName("127.0.0.1"), 0), 128);
    server.setExecutor(Executors.newCachedThreadPool());
    server.createContext("/", Main::handle);
    server.start();
    System.out.println("listening http://127.0.0.1:" + server.getAddress().getPort());
    System.out.flush();
  }

  private static void handle(HttpExchange ex) throws IOException {
    int status = 200;
    String body;
    try {
      body = Json.write(route(ex));
    } catch (Refusal refusal) {
      status = refusal.status;
      body = refusal.getMessage();
    } catch (Throwable error) { // the service reports what the SDK throws; it never absorbs it
      status = 500;
      StringWriter text = new StringWriter();
      error.printStackTrace(new PrintWriter(text));
      body = text.toString();
    }
    byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
    ex.getResponseHeaders()
        .set("Content-Type", status == 200 || status == 201 ? "application/json" : "text/plain");
    ex.sendResponseHeaders(
        status == 200 && ex.getAttribute("created") != null ? 201 : status, bytes.length);
    ex.getResponseBody().write(bytes);
    ex.close();
  }

  private static Object route(HttpExchange ex) throws IOException {
    String method = ex.getRequestMethod();
    String path = ex.getRequestURI().getPath();
    if (method.equals("GET") && path.equals("/")) {
      Map<String, Object> info = new LinkedHashMap<>();
      info.put("name", "toggleslash-sdk-java");
      info.put("capabilities", List.of("integer-accessor"));
      return info;
    }
    if (method.equals("POST") && path.equals("/clients")) {
      Object created = create(readObject(ex));
      ex.setAttribute("created", Boolean.TRUE);
      return created;
    }
    String[] parts = path.split("/", -1);
    if (parts.length != 4 || !parts[0].isEmpty() || !parts[1].equals("clients")) {
      throw new Refusal(404, "no such route");
    }
    Entry entry = CLIENTS.get(parts[2]);
    if (entry == null) {
      throw new Refusal(404, "no such client");
    }
    return switch (method + " " + parts[3]) {
      case "POST ready" -> Map.of("ready", entry.client().ready());
      case "POST evaluate" -> evaluate(entry, readObject(ex));
      case "GET status" -> status(entry.client().status());
      case "GET hook-events" -> Map.of("events", new ArrayList<>(entry.hookEvents()));
      case "GET errors" -> errors(entry);
      case "POST close" -> {
        entry.client().close();
        yield Map.of();
      }
      default -> throw new Refusal(404, "no such route");
    };
  }

  private static Map<String, Object> readObject(HttpExchange ex) throws IOException {
    String text = new String(ex.getRequestBody().readAllBytes(), StandardCharsets.UTF_8);
    Object parsed = text.isEmpty() ? new LinkedHashMap<String, Object>() : Json.parse(text);
    if (!(parsed instanceof Map<?, ?> map)) {
      throw new Refusal(400, "the body is not a JSON object");
    }
    Map<String, Object> out = new LinkedHashMap<>();
    map.forEach((k, v) -> out.put(String.valueOf(k), v));
    return out;
  }

  // ---- POST /clients ----

  private static Object create(Map<String, Object> body) {
    List<Map<String, Object>> hookEvents = new CopyOnWriteArrayList<>();
    List<String> errors = new CopyOnWriteArrayList<>();
    Map<Context, Object> contexts = Collections.synchronizedMap(new IdentityHashMap<>());
    boolean onErrorThrows = Boolean.TRUE.equals(body.get("onErrorThrows"));
    boolean hookThrows = Boolean.TRUE.equals(body.get("hookThrows"));

    Options.Builder options =
        Options.builder()
            .url(String.valueOf(body.get("url")))
            .sdkKey(String.valueOf(body.get("sdkKey")))
            .onError(error -> {
              String message = error.getMessage();
              errors.add(message == null ? error.getClass().getName() : message);
              if (onErrorThrows) {
                throw new IllegalStateException("onError threw, as the test asked");
              }
            });
    if (body.get("stream") instanceof Boolean stream) {
      options.stream(stream);
    }
    if (body.get("pollIntervalMs") instanceof Double ms) {
      options.pollInterval(Duration.ofMillis(ms.longValue()));
    }
    if (body.get("flushIntervalMs") instanceof Double ms) {
      options.flushInterval(Duration.ofMillis(ms.longValue()));
    }
    if (body.get("requestTimeoutMs") instanceof Double ms) {
      options.requestTimeout(Duration.ofMillis(ms.longValue()));
    }
    if (Boolean.TRUE.equals(body.get("hook")) || hookThrows) {
      AfterEvaluation hook =
          event -> {
            Map<String, Object> seen = new LinkedHashMap<>();
            seen.put("flagKey", event.flagKey());
            seen.put("context", contexts.get(event.context()));
            seen.put("value", finite(event.value()));
            seen.put("variationIndex", event.variationIndex());
            seen.put("reason", reason(event.reason()));
            seen.put("etag", event.etag());
            seen.put("flagVersion", event.flagVersion());
            hookEvents.add(seen);
            if (hookThrows) {
              throw new IllegalStateException("the hook threw, as the test asked");
            }
          };
      options.afterEvaluation(hook);
    }
    String id = String.valueOf(CREATED.incrementAndGet());
    CLIENTS.put(id, new Entry(Client.create(options.build()), hookEvents, errors, contexts));
    return Map.of("id", id);
  }

  // ---- POST /clients/{id}/evaluate ----

  private static Object evaluate(Entry entry, Map<String, Object> body) {
    String flag = String.valueOf(body.get("flagKey"));
    Object json = body.get("context");
    Context context = context(json);
    Object fallback = body.get("fallback");
    String accessor = body.get("accessor") instanceof String s ? s : "any";
    Client client = entry.client();
    entry.contexts().put(context, json);
    Detail<?> detail;
    try {
      detail =
          switch (accessor) {
            case "bool" ->
                client.boolVariationDetail(flag, context, require(fallback, Boolean.class));
            case "string" ->
                client.stringVariationDetail(flag, context, require(fallback, String.class));
            case "number" ->
                client.numberVariationDetail(flag, context, require(fallback, Double.class));
            case "int" -> client.longVariationDetail(flag, context, whole(fallback));
            case "json", "any" -> client.jsonVariationDetail(flag, context, fallback);
            default -> throw new Refusal(400, "unknown accessor " + accessor);
          };
    } finally {
      entry.contexts().remove(context);
    }
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("value", finite(detail.value()));
    out.put("variationIndex", detail.variationIndex());
    out.put("reason", reason(detail.reason()));
    return out;
  }

  /**
   * The context the harness sent, valid or not: an invalid one is a case the SDK must answer, so
   * nothing here checks it. A map goes through {@code Context.of}; anything else is an invalid
   * context.
   */
  private static Context context(Object json) {
    if (json instanceof Map<?, ?> map) {
      Map<String, Object> copy = new LinkedHashMap<>();
      map.forEach((k, v) -> copy.put(String.valueOf(k), v));
      return Context.of(copy);
    }
    return Context.parse("null");
  }

  private static <T> T require(Object fallback, Class<T> type) {
    if (!type.isInstance(fallback)) {
      throw new Refusal(400, "a typed accessor needs a fallback of its own kind");
    }
    return type.cast(fallback);
  }

  private static long whole(Object fallback) {
    Double d = require(fallback, Double.class);
    if (d != Math.rint(d) || Math.abs(d) >= 0x1p63) {
      throw new Refusal(400, "the int accessor needs a whole fallback");
    }
    return d.longValue();
  }

  // ---- answers ----

  private static Map<String, Object> status(Status status) {
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("etag", status.etag());
    out.put("lastSyncAt", status.lastSyncAt() == null ? null : status.lastSyncAt().toEpochMilli());
    out.put("stream", status.stream().wire());
    out.put("lastError", status.lastError());
    return out;
  }

  private static Map<String, Object> errors(Entry entry) {
    List<String> messages = new ArrayList<>(entry.errors());
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("count", messages.size());
    out.put("messages", messages);
    return out;
  }

  private static Map<String, Object> reason(Reason reason) {
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("kind", reason.kind().name());
    if (reason.errorCode() != null) {
      out.put("code", reason.errorCode().name());
    }
    if (reason.ruleIndex() != null) {
      out.put("ruleIndex", reason.ruleIndex());
    }
    if (reason.flagKey() != null) {
      out.put("flagKey", reason.flagKey());
    }
    return out;
  }

  /**
   * A copy of a JSON value in which a number that is not finite is null, as {@code JSON.stringify}
   * writes it.
   */
  private static Object finite(Object value) {
    if (value instanceof Double d) {
      return d.isNaN() || d.isInfinite() ? null : d;
    }
    if (value instanceof List<?> list) {
      List<Object> out = new ArrayList<>();
      list.forEach(item -> out.add(finite(item)));
      return out;
    }
    if (value instanceof Map<?, ?> map) {
      Map<String, Object> out = new LinkedHashMap<>();
      map.forEach((k, v) -> out.put(String.valueOf(k), finite(v)));
      return out;
    }
    return value;
  }
}
