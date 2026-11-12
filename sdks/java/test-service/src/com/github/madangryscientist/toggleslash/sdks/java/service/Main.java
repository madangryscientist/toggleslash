package com.github.madangryscientist.toggleslash.sdks.java.service;

import com.github.madangryscientist.toggleslash.sdks.java.Client;
import com.github.madangryscientist.toggleslash.sdks.java.Detail;
import com.github.madangryscientist.toggleslash.sdks.java.EvaluationEvent;
import com.github.madangryscientist.toggleslash.sdks.java.Options;
import com.github.madangryscientist.toggleslash.sdks.java.Status;
import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
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
 * The conformance test service: wraps a {@link Client} in the HTTP protocol of {@code
 * docs/sdk-test-service.md}. It uses only the SDK's public API.
 */
public final class Main {
  private static final Map<String, Entry> CLIENTS = new ConcurrentHashMap<>();
  private static final AtomicInteger NEXT = new AtomicInteger();

  private Main() {}

  /** One client and what the service recorded around it. */
  private static final class Entry {
    final Client client;
    final List<Map<String, Object>> events = Collections.synchronizedList(new ArrayList<>());
    final List<String> errors = Collections.synchronizedList(new ArrayList<>());

    Entry(Client client) {
      this.client = client;
    }
  }

  /**
   * Starts the service on a port the OS picks.
   *
   * @param args ignored
   * @throws IOException if the listener cannot open
   */
  public static void main(String[] args) throws IOException {
    HttpServer server =
        HttpServer.create(new InetSocketAddress(InetAddress.getLoopbackAddress(), 0), 0);
    server.setExecutor(Executors.newCachedThreadPool());
    server.createContext("/", Main::handle);
    server.start();
    System.out.println("listening http://127.0.0.1:" + server.getAddress().getPort());
    System.out.flush();
  }

  private static void handle(HttpExchange exchange) throws IOException {
    int status = 200;
    Object body;
    try {
      String method = exchange.getRequestMethod();
      String path = exchange.getRequestURI().getPath();
      String[] parts = path.split("/");
      if (method.equals("GET") && path.equals("/")) {
        body = Map.of("name", "java", "capabilities", List.of("integer-accessor"));
      } else if (method.equals("POST") && path.equals("/clients")) {
        body = create(parse(exchange));
        status = 201;
      } else if (parts.length == 4 && parts[1].equals("clients")) {
        Entry entry = CLIENTS.get(parts[2]);
        if (entry == null) {
          status = 404;
          body = Map.of("error", "no such client");
        } else {
          body = act(method, parts[3], entry, exchange);
          if (body == null) {
            status = 404;
            body = Map.of("error", "not found");
          }
        }
      } else {
        status = 404;
        body = Map.of("error", "not found");
      }
    } catch (Throwable error) {
      status = 500;
      body = Map.of("error", String.valueOf(error));
    }
    byte[] bytes = Json.write(body).getBytes(StandardCharsets.UTF_8);
    exchange.getResponseHeaders().set("content-type", "application/json");
    exchange.sendResponseHeaders(status, bytes.length);
    try (OutputStream out = exchange.getResponseBody()) {
      out.write(bytes);
    }
  }

  @SuppressWarnings("unchecked")
  private static Map<String, Object> parse(HttpExchange exchange) throws IOException {
    ByteArrayOutputStream buffer = new ByteArrayOutputStream();
    try (InputStream in = exchange.getRequestBody()) {
      in.transferTo(buffer);
    }
    String text = buffer.toString(StandardCharsets.UTF_8);
    if (text.isBlank()) {
      return new LinkedHashMap<>();
    }
    return (Map<String, Object>) Json.parse(text);
  }

  private static Duration millis(Map<String, Object> options, String name) {
    Object value = options.get(name);
    return value instanceof Number ? Duration.ofMillis(((Number) value).longValue()) : null;
  }

  private static Map<String, Object> create(Map<String, Object> options) {
    Entry[] holder = new Entry[1];
    boolean hook = Boolean.TRUE.equals(options.get("hook"));
    boolean hookThrows = Boolean.TRUE.equals(options.get("hookThrows"));
    boolean onErrorThrows = Boolean.TRUE.equals(options.get("onErrorThrows"));
    Options.Builder builder =
        Options.builder((String) options.get("url"), (String) options.get("sdkKey"));
    if (options.get("stream") instanceof Boolean) {
      builder.stream((Boolean) options.get("stream"));
    }
    builder.pollInterval(millis(options, "pollIntervalMs"));
    builder.flushInterval(millis(options, "flushIntervalMs"));
    builder.requestTimeout(millis(options, "requestTimeoutMs"));
    builder.onError(error -> {
      Entry entry = holder[0];
      if (entry != null) {
        entry.errors.add(String.valueOf(error.getMessage()));
      }
      if (onErrorThrows) {
        throw new IllegalStateException("onError throws");
      }
    });
    if (hook || hookThrows) {
      builder.afterEvaluation(event -> {
        Entry entry = holder[0];
        if (entry != null) {
          entry.events.add(eventMap(event));
        }
        if (hookThrows) {
          throw new IllegalStateException("hook throws");
        }
      });
    }
    Entry entry = new Entry(Client.create(builder.build()));
    holder[0] = entry;
    String id = "c" + NEXT.incrementAndGet();
    CLIENTS.put(id, entry);
    return Map.of("id", id);
  }

  private static Map<String, Object> eventMap(EvaluationEvent event) {
    Map<String, Object> map = new LinkedHashMap<>();
    map.put("flagKey", event.flagKey());
    map.put("context", event.context());
    map.put("value", event.value());
    map.put("variationIndex", event.variationIndex());
    map.put("reason", event.reason().toMap());
    map.put("etag", event.etag());
    map.put("flagVersion", event.flagVersion());
    return map;
  }

  private static Object act(String method, String action, Entry entry, HttpExchange exchange)
      throws IOException {
    switch (action) {
      case "ready":
        return method.equals("POST") ? Map.of("ready", entry.client.ready()) : null;
      case "evaluate":
        return method.equals("POST") ? evaluate(entry.client, parse(exchange)) : null;
      case "status":
        return method.equals("GET") ? statusMap(entry.client.status()) : null;
      case "hook-events":
        return method.equals("GET") ? Map.of("events", new ArrayList<>(entry.events)) : null;
      case "errors":
        if (!method.equals("GET")) {
          return null;
        }
        List<String> messages = new ArrayList<>(entry.errors);
        return Map.of("count", messages.size(), "messages", messages);
      case "close":
        if (!method.equals("POST")) {
          return null;
        }
        entry.client.close();
        return Map.of();
      default:
        return null;
    }
  }

  private static Map<String, Object> statusMap(Status status) {
    Map<String, Object> map = new LinkedHashMap<>();
    map.put("etag", status.etag());
    map.put("lastSyncAt", status.lastSyncAt() == null ? null : status.lastSyncAt().toEpochMilli());
    map.put("stream", status.stream().wire());
    map.put("lastError", status.lastError());
    return map;
  }

  private static Map<String, Object> evaluate(Client client, Map<String, Object> request) {
    String flag = (String) request.get("flagKey");
    Object context = request.get("context");
    Object fallback = request.get("fallback");
    String accessor = String.valueOf(request.get("accessor"));
    Detail<?> detail;
    switch (accessor) {
      case "bool":
        detail = client.booleanVariationDetail(flag, context, Boolean.TRUE.equals(fallback));
        break;
      case "string":
        detail = client.stringVariationDetail(flag, context, (String) fallback);
        break;
      case "number":
        detail = client.doubleVariationDetail(flag, context, ((Number) fallback).doubleValue());
        break;
      case "int":
        detail = client.longVariationDetail(flag, context, ((Number) fallback).longValue());
        break;
      case "json":
        detail = client.jsonVariationDetail(flag, context, fallback);
        break;
      default:
        detail = client.variationDetail(flag, context, fallback);
        break;
    }
    Map<String, Object> answer = new LinkedHashMap<>();
    answer.put("value", detail.value());
    answer.put("variationIndex", detail.variationIndex());
    answer.put("reason", detail.reason().toMap());
    return answer;
  }
}
