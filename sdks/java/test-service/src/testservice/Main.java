package testservice;

import com.github.madangryscientist.toggleslash.sdks.java.Client;
import com.github.madangryscientist.toggleslash.sdks.java.Detail;
import com.github.madangryscientist.toggleslash.sdks.java.Options;
import com.github.madangryscientist.toggleslash.sdks.java.Status;
import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import com.github.madangryscientist.toggleslash.sdks.java.json.Utf8;
import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.io.PrintWriter;
import java.io.StringWriter;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.time.Duration;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.Executors;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * The Java SDK's conformance test service: an HTTP server that wraps {@link Client} so the harness
 * in {@code packages/sdk-conformance} can drive it from outside. The protocol is
 * docs/sdk-test-service.md; the accessor mapping is in this directory's README. It catches nothing
 * the SDK throws: a call that raises answers 500 with the error text and the harness fails the
 * scenario.
 */
public final class Main {
  private static final Pattern CLIENT_ROUTE = Pattern.compile("^/clients/([^/]+)/([a-z-]+)$");

  /** A client with what the service recorded about it. */
  private static final class Entry {
    final List<Object> hookEvents = new ArrayList<>();
    final List<String> errors = new ArrayList<>();
    Client client;
  }

  private static final Map<String, Entry> CLIENTS = new ConcurrentHashMap<>();
  private static final AtomicInteger CREATED = new AtomicInteger();

  private Main() {}

  /**
   * Starts the service.
   *
   * @param args ignored
   * @throws IOException when the port cannot be opened
   */
  public static void main(String[] args) throws IOException {
    HttpServer server =
        HttpServer.create(new InetSocketAddress(InetAddress.getByName("127.0.0.1"), 0), 0);
    server.setExecutor(Executors.newCachedThreadPool());
    server.createContext("/", Main::handle);
    server.start();
    System.out.print("listening http://127.0.0.1:" + server.getAddress().getPort() + "\n");
    System.out.flush();
  }

  private static void handle(HttpExchange exchange) throws IOException {
    int status = 200;
    String text;
    try {
      Object answer = route(exchange);
      if (answer instanceof Created) {
        status = 201;
        answer = ((Created) answer).body;
      }
      text = answer == null ? "no such route" : Json.stringify(answer);
      if (answer == null) {
        status = 404;
      }
    } catch (Throwable failure) {
      StringWriter trace = new StringWriter();
      failure.printStackTrace(new PrintWriter(trace));
      status = 500;
      text = trace.toString();
    }
    byte[] bytes = Utf8.encode(text);
    exchange
        .getResponseHeaders()
        .set("Content-Type", status == 500 || status == 404 ? "text/plain" : "application/json");
    exchange.sendResponseHeaders(status, bytes.length);
    try (OutputStream out = exchange.getResponseBody()) {
      out.write(bytes);
    }
  }

  /** A 201 answer. */
  private static final class Created {
    final Object body;

    Created(Object body) {
      this.body = body;
    }
  }

  @SuppressWarnings("unchecked")
  private static Map<String, Object> readBody(HttpExchange exchange) throws IOException {
    ByteArrayOutputStream buffer = new ByteArrayOutputStream();
    try (InputStream in = exchange.getRequestBody()) {
      in.transferTo(buffer);
    }
    byte[] bytes = buffer.toByteArray();
    if (bytes.length == 0) {
      return new LinkedHashMap<>();
    }
    Object body = Json.parse(bytes, 0, bytes.length);
    if (!(body instanceof Map)) {
      throw new IllegalArgumentException("the body is not a JSON object");
    }
    return (Map<String, Object>) body;
  }

  private static Object route(HttpExchange exchange) throws IOException {
    String path = exchange.getRequestURI().getPath();
    String method = exchange.getRequestMethod();
    if (method.equals("GET") && path.equals("/")) {
      Map<String, Object> hello = new LinkedHashMap<>();
      hello.put("name", "toggleslash-sdk (Java)");
      hello.put("capabilities", List.of("integer-accessor"));
      return hello;
    }
    if (method.equals("POST") && path.equals("/clients")) {
      return create(readBody(exchange));
    }
    Matcher match = CLIENT_ROUTE.matcher(path);
    if (!match.matches()) {
      return null;
    }
    Entry entry = CLIENTS.get(match.group(1));
    if (entry == null) {
      return null;
    }
    return clientRoute(entry, method + " " + match.group(2), exchange);
  }

  private static Duration millis(Map<String, Object> body, String name) {
    Object value = body.get(name);
    return value instanceof Number ? Duration.ofMillis(((Number) value).longValue()) : null;
  }

  private static Created create(Map<String, Object> body) {
    Entry entry = new Entry();
    Options.Builder options =
        Options.builder(String.valueOf(body.get("url")), String.valueOf(body.get("sdkKey")))
            .onError(error -> {
              synchronized (entry.errors) {
                entry.errors.add(String.valueOf(error.getMessage()));
              }
              if (Boolean.TRUE.equals(body.get("onErrorThrows"))) {
                throw new IllegalStateException("onError threw, as the test asked");
              }
            });
    if (body.get("stream") instanceof Boolean) {
      options.stream((Boolean) body.get("stream"));
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
    if (Boolean.TRUE.equals(body.get("hook")) || Boolean.TRUE.equals(body.get("hookThrows"))) {
      options.afterEvaluation(event -> {
        Map<String, Object> recorded = new LinkedHashMap<>();
        recorded.put("flagKey", event.flagKey());
        recorded.put("context", event.context());
        recorded.put("value", event.value());
        recorded.put("variationIndex", event.variationIndex());
        recorded.put("reason", event.reason().toJson());
        recorded.put("etag", event.etag());
        recorded.put("flagVersion", event.flagVersion());
        synchronized (entry.hookEvents) {
          entry.hookEvents.add(recorded);
        }
        if (Boolean.TRUE.equals(body.get("hookThrows"))) {
          throw new IllegalStateException("the hook threw, as the test asked");
        }
      });
    }
    String id = String.valueOf(CREATED.incrementAndGet());
    entry.client = Client.create(options.build());
    CLIENTS.put(id, entry);
    Map<String, Object> answer = new LinkedHashMap<>();
    answer.put("id", id);
    return new Created(answer);
  }

  private static Object evaluate(Entry entry, Map<String, Object> body) {
    String flagKey = String.valueOf(body.get("flagKey"));
    Object context = body.get("context");
    Object fallback = body.get("fallback");
    String accessor = String.valueOf(body.get("accessor"));
    Detail<?> detail;
    switch (accessor) {
      case "bool":
        detail = entry.client.boolVariationDetail(flagKey, context, (Boolean) fallback);
        break;
      case "string":
        detail = entry.client.stringVariationDetail(flagKey, context, (String) fallback);
        break;
      case "number":
        detail =
            entry.client.doubleVariationDetail(flagKey, context, ((Number) fallback).doubleValue());
        break;
      case "int":
        detail =
            entry.client.longVariationDetail(flagKey, context, ((Number) fallback).longValue());
        break;
      case "json":
      case "any":
        detail = entry.client.variationDetail(flagKey, context, fallback);
        break;
      default:
        throw new IllegalArgumentException("unknown accessor " + accessor);
    }
    Map<String, Object> answer = new LinkedHashMap<>();
    answer.put("value", detail.value());
    answer.put("variationIndex", detail.variationIndex());
    answer.put("reason", detail.reason().toJson());
    return answer;
  }

  private static Object clientRoute(Entry entry, String key, HttpExchange exchange)
      throws IOException {
    switch (key) {
      case "POST ready":
        return Map.of("ready", entry.client.ready());
      case "POST evaluate":
        return evaluate(entry, readBody(exchange));
      case "GET status":
        return statusOf(entry.client.status());
      case "GET hook-events":
        synchronized (entry.hookEvents) {
          return Map.of("events", new ArrayList<>(entry.hookEvents));
        }
      case "GET errors":
        synchronized (entry.errors) {
          Map<String, Object> answer = new LinkedHashMap<>();
          answer.put("count", entry.errors.size());
          answer.put("messages", new ArrayList<>(entry.errors));
          return answer;
        }
      case "POST close":
        entry.client.close();
        return new LinkedHashMap<String, Object>();
      default:
        return null;
    }
  }

  private static Object statusOf(Status status) {
    Map<String, Object> answer = new LinkedHashMap<>();
    answer.put("etag", status.etag());
    answer.put(
        "lastSyncAt", status.lastSyncAt() == null ? null : status.lastSyncAt().toEpochMilli());
    answer.put("stream", status.stream().value());
    answer.put("lastError", status.lastError());
    return answer;
  }
}
