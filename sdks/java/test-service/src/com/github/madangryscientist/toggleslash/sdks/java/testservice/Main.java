package com.github.madangryscientist.toggleslash.sdks.java.testservice;

import com.github.madangryscientist.toggleslash.sdks.java.Client;
import com.github.madangryscientist.toggleslash.sdks.java.Detail;
import com.github.madangryscientist.toggleslash.sdks.java.EvaluationEvent;
import com.github.madangryscientist.toggleslash.sdks.java.Options;
import com.github.madangryscientist.toggleslash.sdks.java.Reason;
import com.github.madangryscientist.toggleslash.sdks.java.Status;
import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import com.github.madangryscientist.toggleslash.sdks.java.json.Utf8;
import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.io.OutputStream;
import java.io.PrintWriter;
import java.io.StringWriter;
import java.net.InetAddress;
import java.net.InetSocketAddress;
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
 * The Java SDK's conformance test service. The protocol is {@code docs/sdk-test-service.md}; the
 * accessor mapping is in {@code test-service/README.md}. It catches nothing the SDK throws: a call
 * that raises answers 500 with the error text, and the harness fails the scenario.
 */
public final class Main {
  /** One client the harness created, with what the service recorded for it. */
  private record Entry(Client client, List<Map<String, Object>> hookEvents, List<String> errors) {}

  /** A request the service answers with a status other than 200. */
  private static final class Refusal extends RuntimeException {
    private static final long serialVersionUID = 1L;

    private final int status;

    Refusal(int status, String message) {
      super(message);
      this.status = status;
    }
  }

  private static final Map<String, Entry> CLIENTS = new ConcurrentHashMap<>();
  private static final AtomicInteger CREATED = new AtomicInteger();

  private Main() {}

  /**
   * Starts the service and prints the line the harness waits for.
   *
   * @param args ignored
   * @throws IOException if the port cannot be opened
   */
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
    int status = 200;
    String body;
    try {
      body = Json.write(route(exchange));
      if (exchange.getRequestURI().getPath().equals("/clients")
          && exchange.getRequestMethod().equals("POST")) {
        status = 201;
      }
    } catch (Refusal refusal) {
      status = refusal.status;
      body = refusal.getMessage();
    } catch (RuntimeException | StackOverflowError failure) {
      StringWriter text = new StringWriter();
      failure.printStackTrace(new PrintWriter(text));
      status = 500;
      body = text.toString();
    }
    byte[] bytes = Utf8.encode(body);
    exchange
        .getResponseHeaders()
        .set("Content-Type", status < 400 ? "application/json" : "text/plain");
    exchange.sendResponseHeaders(status, bytes.length);
    try (OutputStream out = exchange.getResponseBody()) {
      out.write(bytes);
    }
  }

  private static Object route(HttpExchange exchange) throws IOException {
    String method = exchange.getRequestMethod();
    String path = exchange.getRequestURI().getPath();
    if (method.equals("GET") && path.equals("/")) {
      Map<String, Object> info = new LinkedHashMap<>();
      info.put("name", "toggleslash-sdk (Java)");
      info.put("capabilities", List.of("integer-accessor"));
      return info;
    }
    if (method.equals("POST") && path.equals("/clients")) {
      return create(readBody(exchange));
    }
    String[] parts = path.split("/", -1);
    Entry entry =
        parts.length == 4 && parts[0].isEmpty() && parts[1].equals("clients")
            ? CLIENTS.get(parts[2])
            : null;
    if (entry == null) {
      throw new Refusal(404, "no such client");
    }
    return clientRoute(entry, method + " " + parts[3], exchange);
  }

  private static Map<String, Object> readBody(HttpExchange exchange) throws IOException {
    String text = Utf8.decode(exchange.getRequestBody().readAllBytes());
    Object body = text.isEmpty() ? new LinkedHashMap<String, Object>() : Json.parse(text);
    if (!(body instanceof Map<?, ?> map)) {
      throw new Refusal(400, "the body is not a JSON object");
    }
    Map<String, Object> result = new LinkedHashMap<>();
    for (Map.Entry<?, ?> e : map.entrySet()) {
      result.put((String) e.getKey(), e.getValue());
    }
    return result;
  }

  private static Duration millis(Map<String, Object> body, String name) {
    return body.get(name) instanceof Number n ? Duration.ofMillis(n.longValue()) : null;
  }

  private static Object create(Map<String, Object> body) {
    List<Map<String, Object>> hookEvents = Collections.synchronizedList(new ArrayList<>());
    List<String> errors = Collections.synchronizedList(new ArrayList<>());
    boolean hookThrows = Boolean.TRUE.equals(body.get("hookThrows"));
    boolean onErrorThrows = Boolean.TRUE.equals(body.get("onErrorThrows"));
    Options.Builder options =
        Options.builder()
            .url(String.valueOf(body.get("url")))
            .sdkKey(String.valueOf(body.get("sdkKey")))
            .onError(error -> {
              errors.add(error.getMessage() == null ? error.toString() : error.getMessage());
              if (onErrorThrows) {
                throw new IllegalStateException("onError threw, as the test asked");
              }
            });
    if (body.get("stream") instanceof Boolean stream) {
      options.stream(stream);
    }
    options.pollInterval(millis(body, "pollIntervalMs"));
    options.flushInterval(millis(body, "flushIntervalMs"));
    options.requestTimeout(millis(body, "requestTimeoutMs"));
    if (Boolean.TRUE.equals(body.get("hook")) || hookThrows) {
      options.afterEvaluation(event -> {
        hookEvents.add(eventJson(event));
        if (hookThrows) {
          throw new IllegalStateException("the hook threw, as the test asked");
        }
      });
    }
    String id = String.valueOf(CREATED.incrementAndGet());
    CLIENTS.put(id, new Entry(Client.create(options.build()), hookEvents, errors));
    Map<String, Object> answer = new LinkedHashMap<>();
    answer.put("id", id);
    return answer;
  }

  private static Object clientRoute(Entry entry, String key, HttpExchange exchange)
      throws IOException {
    Map<String, Object> answer = new LinkedHashMap<>();
    switch (key) {
      case "POST ready" -> answer.put("ready", entry.client().ready());
      case "POST evaluate" -> {
        return evaluate(entry.client(), readBody(exchange));
      }
      case "GET status" -> {
        return statusJson(entry.client().status());
      }
      case "GET hook-events" -> {
        synchronized (entry.hookEvents()) {
          answer.put("events", new ArrayList<Object>(entry.hookEvents()));
        }
      }
      case "GET errors" -> {
        synchronized (entry.errors()) {
          answer.put("count", entry.errors().size());
          answer.put("messages", new ArrayList<Object>(entry.errors()));
        }
      }
      case "POST close" -> entry.client().close();
      default -> throw new Refusal(404, "no such route");
    }
    return answer;
  }

  private static Map<String, Object> statusJson(Status status) {
    Map<String, Object> json = new LinkedHashMap<>();
    json.put("etag", status.etag());
    json.put("lastSyncAt", status.lastSyncAt());
    json.put("stream", status.stream().value());
    json.put("lastError", status.lastError());
    return json;
  }

  private static Map<String, Object> reasonJson(Reason reason) {
    Map<String, Object> json = new LinkedHashMap<>();
    json.put("kind", reason.kind().name());
    if (reason.ruleIndex() != null) {
      json.put("ruleIndex", reason.ruleIndex());
    }
    if (reason.flagKey() != null) {
      json.put("flagKey", reason.flagKey());
    }
    if (reason.code() != null) {
      json.put("code", reason.code().name());
    }
    return json;
  }

  private static Map<String, Object> eventJson(EvaluationEvent event) {
    Map<String, Object> json = new LinkedHashMap<>();
    json.put("flagKey", event.flagKey());
    json.put("context", event.context());
    json.put("value", event.value());
    json.put("variationIndex", event.variationIndex());
    json.put("reason", reasonJson(event.reason()));
    json.put("etag", event.etag());
    json.put("flagVersion", event.flagVersion());
    return json;
  }

  private static Object evaluate(Client client, Map<String, Object> body) {
    String flagKey = String.valueOf(body.get("flagKey"));
    Object context = body.get("context");
    Object fallback = body.get("fallback");
    String accessor = String.valueOf(body.getOrDefault("accessor", "any"));
    Detail<?> detail =
        switch (accessor) {
          case "bool" -> client.boolVariationDetail(flagKey, context, asBoolean(fallback));
          case "string" -> client.stringVariationDetail(flagKey, context, asString(fallback));
          case "number" ->
              client.numberVariationDetail(flagKey, context, asNumber(fallback).doubleValue());
          case "int" -> client.intVariationDetail(flagKey, context, asNumber(fallback).longValue());
          case "json", "any" -> client.variationDetail(flagKey, context, fallback);
          default -> throw new Refusal(400, "unknown accessor " + accessor);
        };
    Map<String, Object> answer = new LinkedHashMap<>();
    answer.put("value", detail.value());
    answer.put("variationIndex", detail.variationIndex());
    answer.put("reason", reasonJson(detail.reason()));
    return answer;
  }

  private static boolean asBoolean(Object fallback) {
    if (fallback instanceof Boolean b) {
      return b;
    }
    throw new Refusal(400, "the bool accessor needs a boolean fallback");
  }

  private static String asString(Object fallback) {
    if (fallback instanceof String s) {
      return s;
    }
    throw new Refusal(400, "the string accessor needs a string fallback");
  }

  private static Number asNumber(Object fallback) {
    if (fallback instanceof Number n) {
      return n;
    }
    throw new Refusal(400, "the number accessors need a number fallback");
  }
}
