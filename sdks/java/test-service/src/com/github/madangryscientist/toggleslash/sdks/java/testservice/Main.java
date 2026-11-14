package com.github.madangryscientist.toggleslash.sdks.java.testservice;

import com.github.madangryscientist.toggleslash.sdks.java.Client;
import com.github.madangryscientist.toggleslash.sdks.java.Detail;
import com.github.madangryscientist.toggleslash.sdks.java.EvaluationEvent;
import com.github.madangryscientist.toggleslash.sdks.java.Json;
import com.github.madangryscientist.toggleslash.sdks.java.Options;
import com.github.madangryscientist.toggleslash.sdks.java.Reason;
import com.github.madangryscientist.toggleslash.sdks.java.Status;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Utf8;
import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.io.OutputStream;
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

/** The test service: one HTTP server holding any number of SDK clients. */
public final class Main {
  /** A client the harness drives, with what the service recorded around it. */
  private static final class Entry {
    final List<Object> hookEvents = new ArrayList<>();
    final List<Object> errors = new ArrayList<>();
    volatile Client client;
    // closeInHook / closeInOnError: closing is set when the callback's close starts; calls, threw
    // and
    // maxMs once it has returned. Guarded by this entry.
    boolean closing;
    long closeCalls;
    long closeThrew;
    long closeMs;

    /**
     * The first callback that finds the client stored closes it, timed; anything close throws is
     * caught.
     */
    void closeFromCallback() {
      Client stored;
      synchronized (this) {
        stored = client;
        if (stored == null || closing) {
          return;
        }
        closing = true;
      }
      long started = System.nanoTime();
      long threw = 0;
      try {
        stored.close();
      } catch (RuntimeException e) {
        threw = 1;
      }
      long took = (System.nanoTime() - started) / 1_000_000;
      synchronized (this) {
        closeCalls = 1;
        closeThrew = threw;
        closeMs = took;
      }
    }

    synchronized Map<String, Object> callbackClose() {
      Map<String, Object> out = new LinkedHashMap<>();
      out.put("calls", closeCalls);
      out.put("threw", closeThrew);
      out.put("maxMs", closeMs);
      return out;
    }
  }

  private static final Pattern CLIENT_ROUTE = Pattern.compile("^/clients/([^/]+)/([a-z-]+)$");
  private static final Map<String, Entry> CLIENTS = new ConcurrentHashMap<>();
  private static final AtomicInteger CREATED = new AtomicInteger();

  private Main() {}

  /**
   * Starts the service and prints the line the harness waits for.
   *
   * @param args ignored
   * @throws IOException when the port cannot be bound
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
      Object[] result = route(exchange);
      status = (Integer) result[0];
      body = (String) result[1];
    } catch (Throwable e) {
      status = 500;
      body = String.valueOf(e);
    }
    byte[] bytes = Utf8.encode(body);
    exchange
        .getResponseHeaders()
        .set("Content-Type", status == 500 ? "text/plain" : "application/json");
    exchange.sendResponseHeaders(status, bytes.length == 0 ? -1 : bytes.length);
    if (bytes.length > 0) {
      try (OutputStream out = exchange.getResponseBody()) {
        out.write(bytes);
      }
    }
    exchange.close();
  }

  private static Object[] ok(Object value) {
    return new Object[] {200, Json.stringify(value)};
  }

  private static Object[] route(HttpExchange exchange) throws Exception {
    String method = exchange.getRequestMethod();
    String path = exchange.getRequestURI().getPath();
    if (method.equals("GET") && path.equals("/")) {
      Map<String, Object> hello = new LinkedHashMap<>();
      hello.put("name", "toggleslash-sdk (Java)");
      hello.put("capabilities", List.of("integer-accessor"));
      return ok(hello);
    }
    if (method.equals("POST") && path.equals("/clients")) {
      return create(readBody(exchange));
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
        return evaluate(entry, readBody(exchange));
      case "GET status":
        return ok(status(entry.client.status()));
      case "GET hook-events":
        synchronized (entry.hookEvents) {
          return ok(Map.of("events", new ArrayList<>(entry.hookEvents)));
        }
      case "GET callback-close":
        return ok(entry.callbackClose());
      case "GET errors":
        synchronized (entry.errors) {
          Map<String, Object> errors = new LinkedHashMap<>();
          errors.put("count", (long) entry.errors.size());
          errors.put("messages", new ArrayList<>(entry.errors));
          return ok(errors);
        }
      case "POST close":
        entry.client.close();
        return ok(Map.of());
      default:
        return new Object[] {404, "no such route"};
    }
  }

  @SuppressWarnings("unchecked")
  private static Map<String, Object> readBody(HttpExchange exchange) throws Exception {
    byte[] bytes = exchange.getRequestBody().readAllBytes();
    if (bytes.length == 0) {
      return new LinkedHashMap<>();
    }
    return (Map<String, Object>) Json.parse(Utf8.decode(bytes));
  }

  private static Duration millis(Map<String, Object> body, String name) {
    Object value = body.get(name);
    return value instanceof Double d ? Duration.ofMillis(d.longValue()) : null;
  }

  private static Object[] create(Map<String, Object> body) {
    Entry entry = new Entry();
    Options.Builder options = Options.builder();
    if (body.get("url") instanceof String url) {
      options.url(url);
    }
    if (body.get("sdkKey") instanceof String key) {
      options.sdkKey(key);
    }
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
    boolean hookThrows = Boolean.TRUE.equals(body.get("hookThrows"));
    boolean closeInHook = Boolean.TRUE.equals(body.get("closeInHook"));
    if (Boolean.TRUE.equals(body.get("hook")) || hookThrows || closeInHook) {
      options.afterEvaluation(event -> {
        synchronized (entry.hookEvents) {
          entry.hookEvents.add(hookEvent(event));
        }
        if (closeInHook) {
          entry.closeFromCallback();
        }
        if (hookThrows) {
          throw new IllegalStateException("the hook threw, as the test asked");
        }
        return null;
      });
    }
    boolean onErrorThrows = Boolean.TRUE.equals(body.get("onErrorThrows"));
    boolean closeInOnError = Boolean.TRUE.equals(body.get("closeInOnError"));
    options.onError(error -> {
      synchronized (entry.errors) {
        entry.errors.add(String.valueOf(error.getMessage()));
      }
      if (closeInOnError) {
        entry.closeFromCallback();
      }
      if (onErrorThrows) {
        throw new IllegalStateException("onError threw, as the test asked");
      }
    });
    Client client = Client.create(options.build());
    synchronized (entry) {
      entry.client = client;
    }
    String id = String.valueOf(CREATED.incrementAndGet());
    CLIENTS.put(id, entry);
    return new Object[] {201, Json.stringify(Map.of("id", id))};
  }

  private static Map<String, Object> reason(Reason reason) {
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

  private static Map<String, Object> hookEvent(EvaluationEvent event) {
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("flagKey", event.flagKey());
    out.put("context", event.context());
    out.put("value", event.value());
    out.put("variationIndex", event.variationIndex());
    out.put("reason", reason(event.reason()));
    out.put("etag", event.etag());
    out.put("flagVersion", event.flagVersion());
    return out;
  }

  private static Map<String, Object> status(Status status) {
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("etag", status.etag());
    out.put("lastSyncAt", status.lastSyncAt());
    out.put("stream", status.stream().name());
    out.put("lastError", status.lastError());
    return out;
  }

  private static Object[] evaluate(Entry entry, Map<String, Object> body) {
    String flagKey = String.valueOf(body.get("flagKey"));
    Object context = body.get("context");
    Object fallback = body.get("fallback");
    String accessor = String.valueOf(body.getOrDefault("accessor", "any"));
    Client client = entry.client;
    Detail<?> detail;
    switch (accessor) {
      case "bool":
        detail = client.boolVariationDetail(flagKey, context, (Boolean) fallback);
        break;
      case "string":
        detail = client.stringVariationDetail(flagKey, context, (String) fallback);
        break;
      case "number":
        detail = client.floatVariationDetail(flagKey, context, (Double) fallback);
        break;
      case "int":
        detail = client.intVariationDetail(flagKey, context, ((Double) fallback).longValue());
        break;
      case "json":
        detail = client.jsonVariationDetail(flagKey, context, fallback);
        break;
      case "any":
        detail = client.variationDetail(flagKey, context, fallback);
        break;
      default:
        return new Object[] {400, "unknown accessor " + accessor};
    }
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("value", detail.value());
    out.put("variationIndex", detail.variationIndex());
    out.put("reason", reason(detail.reason()));
    return ok(out);
  }
}
