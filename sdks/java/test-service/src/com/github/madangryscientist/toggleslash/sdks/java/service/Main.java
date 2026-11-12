package com.github.madangryscientist.toggleslash.sdks.java.service;

import com.github.madangryscientist.toggleslash.sdks.java.Client;
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
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CopyOnWriteArrayList;
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
public final class Main {
  /** A client and what the service recorded of it. */
  private record Entry(Client client, List<Map<String, Object>> hookEvents, List<String> errors) {}

  private static final Pattern CLIENT_ROUTE = Pattern.compile("^/clients/([^/]+)/([a-z-]+)$");

  private final Map<String, Entry> clients = new ConcurrentHashMap<>();
  private final AtomicInteger created = new AtomicInteger();

  private Main() {}

  /** An answer: a status and a JSON body. */
  private record Answer(int status, Object body) {}

  /** A request the service cannot take: a typed accessor given a fallback of another kind. */
  private static final class BadRequest extends RuntimeException {
    private static final long serialVersionUID = 1L;

    BadRequest(String message) {
      super(message);
    }
  }

  /**
   * Starts the service on a port the system picks and prints the line the harness waits for.
   *
   * @param args none
   * @throws IOException if the port cannot be opened
   */
  public static void main(String[] args) throws IOException {
    Main service = new Main();
    HttpServer server =
        HttpServer.create(new InetSocketAddress(InetAddress.getLoopbackAddress(), 0), 0);
    server.setExecutor(
        Executors.newCachedThreadPool(runnable -> {
          Thread thread = new Thread(runnable, "service-request");
          thread.setDaemon(true);
          return thread;
        }));
    server.createContext("/", service::handle);
    server.start();
    System.out.println("listening http://127.0.0.1:" + server.getAddress().getPort());
    System.out.flush();
  }

  private void handle(HttpExchange exchange) throws IOException {
    int status;
    byte[] bytes;
    try {
      byte[] raw = exchange.getRequestBody().readAllBytes();
      Answer answer = route(exchange.getRequestMethod(), exchange.getRequestURI().getPath(), raw);
      status = answer.status();
      bytes = JsonOut.write(answer.body()).getBytes(StandardCharsets.UTF_8);
    } catch (BadRequest e) {
      status = 400;
      bytes = String.valueOf(e.getMessage()).getBytes(StandardCharsets.UTF_8);
    } catch (Throwable e) {
      status = 500;
      StringWriter text = new StringWriter();
      e.printStackTrace(new PrintWriter(text));
      bytes = text.toString().getBytes(StandardCharsets.UTF_8);
    }
    exchange.getResponseHeaders().set("Content-Type", "application/json");
    exchange.sendResponseHeaders(status, bytes.length == 0 ? -1 : bytes.length);
    if (bytes.length > 0) {
      exchange.getResponseBody().write(bytes);
    }
    exchange.close();
  }

  private Answer route(String method, String path, byte[] raw) {
    if (method.equals("GET") && path.equals("/")) {
      return new Answer(
          200,
          Map.of("name", "toggleslash-sdk (Java)", "capabilities", List.of("integer-accessor")));
    }
    if (method.equals("POST") && path.equals("/clients")) {
      return create(body(raw));
    }
    Matcher match = CLIENT_ROUTE.matcher(path);
    Entry entry = match.matches() ? clients.get(match.group(1)) : null;
    if (entry == null) {
      return new Answer(404, "no such client");
    }
    return clientRoute(entry, method + " " + match.group(2), raw);
  }

  @SuppressWarnings("unchecked")
  private static Map<String, Object> body(byte[] raw) {
    String text = new String(raw, StandardCharsets.UTF_8);
    Object parsed = text.isEmpty() ? new LinkedHashMap<String, Object>() : Json.parse(text);
    if (!(parsed instanceof Map)) {
      throw new IllegalArgumentException("the body is not a JSON object");
    }
    return (Map<String, Object>) parsed;
  }

  private static Duration millis(Map<String, Object> body, String name) {
    return body.get(name) instanceof Number n ? Duration.ofMillis(n.longValue()) : null;
  }

  private Answer create(Map<String, Object> body) {
    List<Map<String, Object>> hookEvents = new CopyOnWriteArrayList<>();
    List<String> errors = new CopyOnWriteArrayList<>();
    Options.Builder options =
        Options.builder(String.valueOf(body.get("url")), String.valueOf(body.get("sdkKey")));
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
    boolean onErrorThrows = Boolean.TRUE.equals(body.get("onErrorThrows"));
    options.onError(error -> {
      errors.add(error.getMessage() == null ? error.toString() : error.getMessage());
      if (onErrorThrows) {
        throw new IllegalStateException("onError threw, as the test asked");
      }
    });
    boolean hookThrows = Boolean.TRUE.equals(body.get("hookThrows"));
    if (Boolean.TRUE.equals(body.get("hook")) || hookThrows) {
      options.afterEvaluation(event -> {
        hookEvents.add(eventJson(event));
        if (hookThrows) {
          throw new IllegalStateException("the hook threw, as the test asked");
        }
      });
    }
    String id = String.valueOf(created.incrementAndGet());
    clients.put(id, new Entry(Client.create(options.build()), hookEvents, errors));
    return new Answer(201, Map.of("id", id));
  }

  private Answer clientRoute(Entry entry, String key, byte[] raw) {
    switch (key) {
      case "POST ready":
        return new Answer(200, Map.of("ready", entry.client().ready()));
      case "POST evaluate":
        return evaluate(entry, body(raw));
      case "GET status":
        return new Answer(200, statusJson(entry.client().status()));
      case "GET hook-events":
        return new Answer(200, Map.of("events", new ArrayList<>(entry.hookEvents())));
      case "GET errors":
        {
          List<String> messages = new ArrayList<>(entry.errors());
          Map<String, Object> out = new LinkedHashMap<>();
          out.put("count", messages.size());
          out.put("messages", messages);
          return new Answer(200, out);
        }
      case "POST close":
        entry.client().close();
        return new Answer(200, Map.of());
      default:
        return new Answer(404, "no such route");
    }
  }

  /** Each accessor of the protocol, on the SDK's method of the same kind (see the README). */
  private Answer evaluate(Entry entry, Map<String, Object> body) {
    Client client = entry.client();
    String flag = String.valueOf(body.get("flagKey"));
    Object context = body.get("context");
    Object fallback = body.get("fallback");
    String accessor = String.valueOf(body.get("accessor"));
    Detail<?> detail;
    switch (accessor) {
      case "bool":
        if (!(fallback instanceof Boolean b)) {
          throw new BadRequest("bool needs a boolean fallback");
        }
        detail = client.boolVariationDetail(flag, context, b);
        break;
      case "string":
        if (!(fallback instanceof String s)) {
          throw new BadRequest("string needs a string fallback");
        }
        detail = client.stringVariationDetail(flag, context, s);
        break;
      case "number":
        if (!(fallback instanceof Number n)) {
          throw new BadRequest("number needs a number fallback");
        }
        detail = client.floatVariationDetail(flag, context, n.doubleValue());
        break;
      case "int":
        if (!(fallback instanceof Number n)) {
          throw new BadRequest("int needs a number fallback");
        }
        detail = client.intVariationDetail(flag, context, n.longValue());
        break;
      case "json":
      case "any":
        detail = client.variationDetail(flag, context, fallback);
        break;
      default:
        throw new BadRequest("no such accessor: " + accessor);
    }
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("value", detail.value());
    out.put("variationIndex", detail.variationIndex());
    out.put("reason", reasonJson(detail.reason()));
    return new Answer(200, out);
  }

  private static Map<String, Object> reasonJson(Reason reason) {
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("kind", reason.kind().name());
    switch (reason.kind()) {
      case RULE_MATCH -> out.put("ruleIndex", reason.ruleIndex());
      case PREREQUISITE_FAILED -> out.put("flagKey", reason.flagKey());
      case ERROR -> out.put("code", reason.errorCode().name());
      default -> {
        // The other kinds carry nothing more.
      }
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

  private static Map<String, Object> statusJson(Status status) {
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("etag", status.etag());
    out.put("lastSyncAt", status.lastSyncAt() == null ? null : status.lastSyncAt().toEpochMilli());
    out.put("stream", status.stream().value());
    out.put("lastError", status.lastError());
    return out;
  }
}
