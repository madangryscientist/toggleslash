package com.github.madangryscientist.toggleslash.sdks.java.service;

import com.github.madangryscientist.toggleslash.sdks.java.Detail;
import com.github.madangryscientist.toggleslash.sdks.java.EvaluationEvent;
import com.github.madangryscientist.toggleslash.sdks.java.Json;
import com.github.madangryscientist.toggleslash.sdks.java.Options;
import com.github.madangryscientist.toggleslash.sdks.java.Reason;
import com.github.madangryscientist.toggleslash.sdks.java.Status;
import com.github.madangryscientist.toggleslash.sdks.java.ToggleSlashClient;
import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.Executors;
import java.util.concurrent.atomic.AtomicInteger;

/**
 * The conformance test service (docs/sdk-test-service.md) for the Java SDK: a small JSON server
 * over the JDK's own HTTP server that holds any number of clients and maps each request onto the
 * SDK. test-service/README.md says how each accessor maps to a method.
 */
public final class Main {
  private static final String PREFIX = "/clients/";

  /** A client the service holds, and what the harness reads back from it. */
  private static final class Held {
    final List<Object> events = Collections.synchronizedList(new ArrayList<>());
    final List<String> errors = Collections.synchronizedList(new ArrayList<>());
    volatile ToggleSlashClient client;
  }

  /** A request the service refuses. */
  private static final class BadRequest extends RuntimeException {
    private static final long serialVersionUID = 1L;

    BadRequest(String message) {
      super(message);
    }
  }

  private final Map<String, Held> clients = new ConcurrentHashMap<>();
  private final AtomicInteger counter = new AtomicInteger();

  private Main() {}

  /**
   * Starts the service and prints the line the harness waits for.
   *
   * @param args none
   * @throws IOException if the port cannot be bound
   */
  public static void main(String[] args) throws IOException {
    Main service = new Main();
    HttpServer server =
        HttpServer.create(new InetSocketAddress(InetAddress.getLoopbackAddress(), 0), 128);
    server.setExecutor(
        Executors.newCachedThreadPool(runnable -> {
          Thread t = new Thread(runnable, "service-request");
          t.setDaemon(true);
          return t;
        }));
    server.createContext("/", service::handle);
    server.start();
    System.out.println("listening http://127.0.0.1:" + server.getAddress().getPort());
    System.out.flush();
  }

  private void handle(HttpExchange exchange) throws IOException {
    int code = 200;
    Object answer;
    try {
      String method = exchange.getRequestMethod();
      String path = exchange.getRequestURI().getPath();
      byte[] raw = exchange.getRequestBody().readAllBytes();
      Object body = raw.length == 0 ? null : Json.parse(new String(raw, StandardCharsets.UTF_8));
      if (method.equals("GET") && path.equals("/")) {
        Map<String, Object> info = new LinkedHashMap<>();
        info.put("name", "toggleslash-java");
        info.put("capabilities", List.of("integer-accessor"));
        answer = info;
      } else if (method.equals("POST") && path.equals("/clients")) {
        code = 201;
        answer = create(object(body));
      } else if (path.startsWith(PREFIX)) {
        answer = clientRoute(method, path.substring(PREFIX.length()), body);
      } else {
        code = 404;
        answer = error("not found");
      }
    } catch (BadRequest e) {
      code = 400;
      answer = error(e.getMessage());
    } catch (Throwable e) {
      code = 500;
      answer = error(e.getClass().getName() + ": " + e.getMessage());
    }
    byte[] out = Json.write(answer).getBytes(StandardCharsets.UTF_8);
    exchange.getResponseHeaders().set("Content-Type", "application/json");
    exchange.sendResponseHeaders(code, out.length == 0 ? -1 : out.length);
    try (OutputStream stream = exchange.getResponseBody()) {
      stream.write(out);
    }
  }

  private static Map<String, Object> error(String message) {
    Map<String, Object> map = new LinkedHashMap<>();
    map.put("error", message);
    return map;
  }

  @SuppressWarnings("unchecked")
  private static Map<String, Object> object(Object body) {
    if (!(body instanceof Map)) {
      throw new BadRequest("a JSON object is required");
    }
    return (Map<String, Object>) body;
  }

  private Object clientRoute(String method, String rest, Object body) {
    int slash = rest.indexOf('/');
    if (slash < 0) {
      throw new BadRequest("unknown route");
    }
    Held held = clients.get(rest.substring(0, slash));
    if (held == null) {
      throw new BadRequest("unknown client");
    }
    String action = rest.substring(slash + 1);
    Map<String, Object> out = new LinkedHashMap<>();
    switch (method + " " + action) {
      case "POST ready":
        out.put("ready", held.client.ready());
        return out;
      case "POST evaluate":
        return evaluate(held, object(body));
      case "GET status":
        return status(held.client.status());
      case "GET hook-events":
        out.put("events", new ArrayList<>(held.events));
        return out;
      case "GET errors":
        List<String> messages = new ArrayList<>(held.errors);
        out.put("count", messages.size());
        out.put("messages", messages);
        return out;
      case "POST close":
        held.client.close();
        return out;
      default:
        throw new BadRequest("unknown route");
    }
  }

  private Object create(Map<String, Object> options) {
    Held held = new Held();
    Options.Builder builder =
        Options.builder(text(options.get("url")), text(options.get("sdkKey")));
    if (options.get("stream") instanceof Boolean on) {
      builder.stream(on);
    }
    duration(options, "pollIntervalMs", builder::pollInterval);
    duration(options, "flushIntervalMs", builder::flushInterval);
    duration(options, "requestTimeoutMs", builder::requestTimeout);
    boolean hookThrows = Boolean.TRUE.equals(options.get("hookThrows"));
    boolean onErrorThrows = Boolean.TRUE.equals(options.get("onErrorThrows"));
    if (Boolean.TRUE.equals(options.get("hook")) || hookThrows) {
      builder.afterEvaluation(event -> {
        held.events.add(eventJson(event));
        if (hookThrows) {
          throw new IllegalStateException("the hook threw");
        }
      });
    }
    builder.onError(error -> {
      held.errors.add(String.valueOf(error.getMessage()));
      if (onErrorThrows) {
        throw new IllegalStateException("onError threw");
      }
    });
    held.client = ToggleSlashClient.create(builder.build());
    String id = "c" + counter.incrementAndGet();
    clients.put(id, held);
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("id", id);
    return out;
  }

  private static String text(Object value) {
    return value instanceof String s ? s : "";
  }

  private static void duration(
      Map<String, Object> options, String key, java.util.function.Consumer<Duration> sink) {
    if (options.get(key) instanceof Number n) {
      sink.accept(Duration.ofMillis(n.longValue()));
    }
  }

  private Object evaluate(Held held, Map<String, Object> request) {
    String flagKey = text(request.get("flagKey"));
    Object context = request.get("context");
    Object fallback = request.get("fallback");
    String accessor = text(request.get("accessor"));
    ToggleSlashClient client = held.client;
    Detail<?> detail;
    switch (accessor) {
      case "bool":
        detail =
            client.boolVariationDetail(
                flagKey, context, (Boolean) require(fallback, Boolean.class));
        break;
      case "string":
        detail =
            client.stringVariationDetail(
                flagKey, context, (String) require(fallback, String.class));
        break;
      case "number":
        detail =
            client.doubleVariationDetail(
                flagKey, context, ((Number) require(fallback, Number.class)).doubleValue());
        break;
      case "int":
        detail =
            client.intVariationDetail(
                flagKey, context, ((Number) require(fallback, Number.class)).longValue());
        break;
      case "json":
      case "any":
        detail = client.variationDetail(flagKey, context, fallback);
        break;
      default:
        throw new BadRequest("unknown accessor");
    }
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("value", detail.value());
    out.put("variationIndex", detail.variationIndex());
    out.put("reason", reasonJson(detail.reason()));
    return out;
  }

  private static Object require(Object fallback, Class<?> type) {
    if (!type.isInstance(fallback)) {
      throw new BadRequest("the fallback must be a " + type.getSimpleName());
    }
    return fallback;
  }

  private static Map<String, Object> reasonJson(Reason reason) {
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("kind", reason.kind().name());
    if (reason.ruleIndex() != null) {
      out.put("ruleIndex", reason.ruleIndex());
    }
    if (reason.flagKey() != null) {
      out.put("flagKey", reason.flagKey());
    }
    if (reason.errorCode() != null) {
      out.put("code", reason.errorCode().name());
    }
    return out;
  }

  private static Map<String, Object> eventJson(EvaluationEvent event) {
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("flagKey", event.flagKey());
    out.put("context", event.context());
    out.put("value", event.value());
    out.put("variationIndex", event.variationIndex());
    out.put("reason", reasonJson(event.reason()));
    out.put("etag", event.etag());
    out.put("flagVersion", event.flagVersion());
    return out;
  }

  private static Map<String, Object> status(Status status) {
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("etag", status.etag());
    out.put("lastSyncAt", status.lastSyncAt() == null ? null : status.lastSyncAt().toEpochMilli());
    out.put("stream", status.stream().value());
    out.put("lastError", status.lastError());
    return out;
  }
}
