package com.github.madangryscientist.toggleslash.sdks.java.testservice;

import com.github.madangryscientist.toggleslash.sdks.java.Client;
import com.github.madangryscientist.toggleslash.sdks.java.Context;
import com.github.madangryscientist.toggleslash.sdks.java.Detail;
import com.github.madangryscientist.toggleslash.sdks.java.EvaluationEvent;
import com.github.madangryscientist.toggleslash.sdks.java.Options;
import com.github.madangryscientist.toggleslash.sdks.java.Status;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Json;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;
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
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.Executors;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * The Java SDK's conformance test service: an HTTP server that wraps {@link Client} so the harness
 * in {@code packages/sdk-conformance} can drive it from outside. The protocol is {@code
 * docs/sdk-test-service.md}; the accessor mapping is in this directory's README. It catches nothing
 * the SDK throws: a call that raises answers 500 with the error text, and the harness fails the
 * scenario.
 */
public final class TestService {
  private static final Pattern CLIENT_ROUTE = Pattern.compile("^/clients/([^/]+)/([a-z-]+)$");

  /** One client and what the service saw it do. */
  private record Entry(Client client, List<EvaluationEvent> hookEvents, List<String> errors) {}

  private final Map<String, Entry> clients = new ConcurrentHashMap<>();
  private final AtomicInteger created = new AtomicInteger();

  private TestService() {}

  /** Starts the service on a port the OS picks and prints the line the harness waits for. */
  public static void main(String[] args) throws Exception {
    TestService service = new TestService();
    HttpServer server =
        HttpServer.create(new InetSocketAddress(InetAddress.getLoopbackAddress(), 0), 0);
    server.setExecutor(Executors.newCachedThreadPool());
    server.createContext("/", exchange -> service.handle(exchange));
    server.start();
    System.out.println("listening http://127.0.0.1:" + server.getAddress().getPort());
    System.out.flush();
    new CountDownLatch(1).await();
  }

  private void handle(HttpExchange exchange) throws IOException {
    int status = 200;
    String body;
    try {
      Object[] answer = route(exchange);
      status = (Integer) answer[0];
      body = (String) answer[1];
    } catch (Throwable e) {
      StringWriter text = new StringWriter();
      e.printStackTrace(new PrintWriter(text));
      status = 500;
      body = text.toString();
    }
    byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
    exchange
        .getResponseHeaders()
        .set("Content-Type", status == 500 ? "text/plain" : "application/json");
    exchange.sendResponseHeaders(status, bytes.length == 0 ? -1 : bytes.length);
    if (bytes.length > 0) {
      exchange.getResponseBody().write(bytes);
    }
    exchange.close();
  }

  private static Object[] json(int status, Object value) {
    return new Object[] {status, Json.write(value)};
  }

  private Object[] route(HttpExchange exchange) throws IOException {
    String method = exchange.getRequestMethod();
    String path = exchange.getRequestURI().getPath();
    byte[] raw = exchange.getRequestBody().readAllBytes();
    if (method.equals("GET") && path.equals("/")) {
      Map<String, Object> info = new LinkedHashMap<>();
      info.put("name", "toggleslash-sdk-java");
      info.put("capabilities", List.of("integer-accessor"));
      return json(200, info);
    }
    if (method.equals("POST") && path.equals("/clients")) {
      return create(readBody(raw));
    }
    Matcher match = CLIENT_ROUTE.matcher(path);
    Entry entry = match.matches() ? clients.get(match.group(1)) : null;
    if (entry == null) {
      return new Object[] {404, "no such client"};
    }
    return clientRoute(entry, method + " " + match.group(2), raw);
  }

  @SuppressWarnings("unchecked")
  private static Map<String, Object> readBody(byte[] raw) {
    Object body = raw.length == 0 ? new LinkedHashMap<String, Object>() : Json.parse(raw);
    if (!(body instanceof Map)) {
      throw new IllegalArgumentException("the body is not a JSON object");
    }
    return (Map<String, Object>) body;
  }

  private static Duration millis(Map<String, Object> body, String name) {
    return body.get(name) instanceof Double d ? Duration.ofMillis(d.longValue()) : null;
  }

  private Object[] create(Map<String, Object> body) {
    List<EvaluationEvent> hookEvents = Collections.synchronizedList(new ArrayList<>());
    List<String> errors = Collections.synchronizedList(new ArrayList<>());
    Options.Builder options =
        Options.builder(String.valueOf(body.get("url")), String.valueOf(body.get("sdkKey")))
            .pollInterval(millis(body, "pollIntervalMs"))
            .flushInterval(millis(body, "flushIntervalMs"))
            .requestTimeout(millis(body, "requestTimeoutMs"))
            .onError(
                error -> {
                  errors.add(error.getMessage() != null ? error.getMessage() : error.toString());
                  if (Boolean.TRUE.equals(body.get("onErrorThrows"))) {
                    throw new IllegalStateException("onError threw, as the test asked");
                  }
                });
    if (body.get("stream") instanceof Boolean stream) {
      options.stream(stream);
    }
    boolean hookThrows = Boolean.TRUE.equals(body.get("hookThrows"));
    if (Boolean.TRUE.equals(body.get("hook")) || hookThrows) {
      options.afterEvaluation(
          event -> {
            hookEvents.add(event);
            if (hookThrows) {
              throw new IllegalStateException("the hook threw, as the test asked");
            }
          });
    }
    String id = String.valueOf(created.incrementAndGet());
    clients.put(id, new Entry(Client.create(options.build()), hookEvents, errors));
    Map<String, Object> answer = new LinkedHashMap<>();
    answer.put("id", id);
    return json(201, answer);
  }

  private static Map<String, Object> reasonJson(Reason reason) {
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("kind", reason.kind().value());
    reason.ruleIndex().ifPresent(i -> out.put("ruleIndex", i));
    reason.flagKey().ifPresent(k -> out.put("flagKey", k));
    reason.code().ifPresent(c -> out.put("code", c.value()));
    return out;
  }

  private static Map<String, Object> detailJson(Detail<?> detail) {
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("value", detail.value());
    out.put(
        "variationIndex",
        detail.variationIndex().isPresent() ? detail.variationIndex().getAsInt() : null);
    out.put("reason", reasonJson(detail.reason()));
    return out;
  }

  /** Each accessor of the protocol is one typed method of the client; see the README. */
  private static Detail<?> evaluate(Client client, Map<String, Object> body) {
    String flag = String.valueOf(body.get("flagKey"));
    // The service hands the SDK whatever JSON the harness sent, valid or not: an invalid context is
    // one
    // of the cases the SDK must answer (ERROR INVALID_CONTEXT), so nothing here checks it.
    Context context = Context.fromJson(Json.write(body.get("context")));
    Object fallback = body.get("fallback");
    switch (String.valueOf(body.get("accessor"))) {
      case "bool":
        return client.boolVariationDetail(flag, context, (Boolean) fallback);
      case "string":
        return client.stringVariationDetail(flag, context, (String) fallback);
      case "number":
        return client.doubleVariationDetail(flag, context, (Double) fallback);
      case "int":
        return client.intVariationDetail(flag, context, ((Double) fallback).longValue());
      case "json":
      case "any":
        return client.jsonVariationDetail(flag, context, fallback);
      default:
        throw new IllegalArgumentException("unknown accessor");
    }
  }

  private static Map<String, Object> statusJson(Status status) {
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("etag", status.etag().orElse(null));
    out.put("lastSyncAt", status.lastSyncAt().map(t -> (Object) t.toEpochMilli()).orElse(null));
    out.put("stream", status.stream().value());
    out.put("lastError", status.lastError().orElse(null));
    return out;
  }

  private static Map<String, Object> eventJson(EvaluationEvent event) {
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("flagKey", event.flagKey());
    out.put(
        "context",
        Json.parse(event.context() == null ? "null" : String.valueOf(event.context().toJson())));
    out.put("value", event.value());
    out.put(
        "variationIndex",
        event.variationIndex().isPresent() ? event.variationIndex().getAsInt() : null);
    out.put("reason", reasonJson(event.reason()));
    out.put("etag", event.etag().orElse(null));
    out.put("flagVersion", event.flagVersion().orElse(null));
    return out;
  }

  private Object[] clientRoute(Entry entry, String key, byte[] raw) {
    switch (key) {
      case "POST ready":
        return json(200, Map.of("ready", entry.client().ready()));
      case "POST evaluate":
        return json(200, detailJson(evaluate(entry.client(), readBody(raw))));
      case "GET status":
        return json(200, statusJson(entry.client().status()));
      case "GET hook-events":
        {
          List<Object> events = new ArrayList<>();
          synchronized (entry.hookEvents()) {
            for (EvaluationEvent event : entry.hookEvents()) {
              events.add(eventJson(event));
            }
          }
          return json(200, Map.of("events", events));
        }
      case "GET errors":
        {
          List<Object> messages;
          synchronized (entry.errors()) {
            messages = new ArrayList<>(entry.errors());
          }
          Map<String, Object> out = new LinkedHashMap<>();
          out.put("count", messages.size());
          out.put("messages", messages);
          return json(200, out);
        }
      case "POST close":
        entry.client().close();
        return json(200, Map.of());
      default:
        return new Object[] {404, "no such route"};
    }
  }
}
