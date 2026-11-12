package com.github.madangryscientist.toggleslash.sdks.java;

import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import com.sun.net.httpserver.HttpsConfigurator;
import com.sun.net.httpserver.HttpsServer;
import java.io.IOException;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.TreeMap;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.Executors;
import javax.net.ssl.SSLContext;

/**
 * A scripted ToggleSlash server for the SDK's own tests, on the JDK's HTTP server and a loopback
 * port. Each route has a handler a test replaces; the defaults serve a ruleset with an ETag and
 * 304, a stream that stays open, and events.
 */
final class FakeServer implements AutoCloseable {
  /** One request as the server received it; header names are lower case. */
  record Request(String method, String path, Map<String, String> headers, String body) {}

  /** Answers one request. */
  @FunctionalInterface
  interface Handler {
    void handle(HttpExchange exchange, Request request) throws Exception;
  }

  private record Served(String body, String etag) {}

  final List<Request> requests = new CopyOnWriteArrayList<>();
  volatile Handler ruleset = this::rulesetAnswer;
  volatile Handler stream = this::streamAnswer;
  volatile Handler events = (exchange, request) -> send(exchange, 200, Map.of(), "{}");
  private final HttpServer server;
  private final List<OutputStream> open = new CopyOnWriteArrayList<>();
  private final CountDownLatch released = new CountDownLatch(1);
  private final boolean tls;
  private volatile Served served = new Served(rulesetJson(1, "checkout"), "\"v1\"");

  FakeServer() {
    this(null);
  }

  FakeServer(SSLContext tlsContext) {
    try {
      InetSocketAddress address = new InetSocketAddress(InetAddress.getByName("127.0.0.1"), 0);
      if (tlsContext == null) {
        server = HttpServer.create(address, 0);
        tls = false;
      } else {
        HttpsServer secure = HttpsServer.create(address, 0);
        secure.setHttpsConfigurator(new HttpsConfigurator(tlsContext));
        server = secure;
        tls = true;
      }
    } catch (IOException e) {
      throw new IllegalStateException(e);
    }
    server.setExecutor(
        Executors.newCachedThreadPool(runnable -> {
          Thread thread = new Thread(runnable, "fake-server");
          thread.setDaemon(true);
          return thread;
        }));
    server.createContext("/", this::dispatch);
    server.start();
  }

  String url() {
    return (tls ? "https" : "http") + "://127.0.0.1:" + server.getAddress().getPort();
  }

  /** A ruleset of boolean flags that all serve {@code true}, each at the given version. */
  static String rulesetJson(int version, String... flagKeys) {
    StringBuilder json = new StringBuilder("{\"flags\":[");
    for (int i = 0; i < flagKeys.length; i++) {
      if (i > 0) {
        json.append(',');
      }
      json.append(flag(flagKeys[i], "[false,true]", 1, version));
    }
    return json.append("],\"segments\":[]}").toString();
  }

  /** A flag whose fallthrough serves {@code variation} out of the {@code variations} JSON array. */
  static String flag(String key, String variations, int variation, int version) {
    return "{\"key\":\""
        + key
        + "\",\"variations\":"
        + variations
        + ",\"on\":true,\"offVariation\":0,\"targets\":[],\"prerequisites\":[],\"rules\":[],"
        + "\"fallthrough\":{\"variation\":"
        + variation
        + "},\"salt\":\"s\",\"version\":"
        + version
        + "}";
  }

  /** Replaces what the default ruleset route serves. */
  void serve(String json, String tag) {
    this.served = new Served(json, tag);
  }

  String etag() {
    return served.etag();
  }

  /** Requests to one route, in arrival order. */
  List<Request> to(String path) {
    List<Request> found = new ArrayList<>();
    for (Request request : requests) {
      if (request.path().equals(path)) {
        found.add(request);
      }
    }
    return found;
  }

  /** Writes raw text to every open stream. */
  void push(String raw) throws IOException {
    for (OutputStream out : open) {
      out.write(raw.getBytes(StandardCharsets.UTF_8));
      out.flush();
    }
  }

  /** Blocks the calling handler until the server is closed. */
  void hang() throws InterruptedException {
    released.await();
  }

  static void send(HttpExchange exchange, int status, Map<String, String> headers, String text)
      throws IOException {
    byte[] bytes = text.getBytes(StandardCharsets.UTF_8);
    headers.forEach((name, value) -> exchange.getResponseHeaders().set(name, value));
    if (status == 304 || bytes.length == 0) {
      exchange.sendResponseHeaders(status, -1);
    } else {
      exchange.sendResponseHeaders(status, bytes.length);
      try (OutputStream out = exchange.getResponseBody()) {
        out.write(bytes);
      }
    }
    exchange.close();
  }

  private void dispatch(HttpExchange exchange) throws IOException {
    Map<String, String> headers = new TreeMap<>();
    exchange
        .getRequestHeaders()
        .forEach((name, values) -> headers.put(name.toLowerCase(Locale.ROOT), values.get(0)));
    String text = new String(exchange.getRequestBody().readAllBytes(), StandardCharsets.UTF_8);
    Request request =
        new Request(exchange.getRequestMethod(), exchange.getRequestURI().getPath(), headers, text);
    requests.add(request);
    Handler handler =
        switch (request.path()) {
          case "/sdk/v1/ruleset" -> ruleset;
          case "/sdk/v1/stream" -> stream;
          case "/sdk/v1/events" -> events;
          default -> (e, r) -> send(e, 404, Map.of(), "no such route");
        };
    try {
      handler.handle(exchange, request);
    } catch (IOException | InterruptedException gone) {
      exchange.close();
    } catch (Exception failure) {
      exchange.close();
      throw new IllegalStateException(failure);
    }
  }

  private void rulesetAnswer(HttpExchange exchange, Request request) throws IOException {
    Served now = served;
    if (now.etag().equals(request.headers().get("if-none-match"))) {
      send(exchange, 304, Map.of(), "");
      return;
    }
    send(exchange, 200, Map.of("ETag", now.etag(), "Content-Type", "application/json"), now.body());
  }

  private void streamAnswer(HttpExchange exchange, Request request)
      throws IOException, InterruptedException {
    exchange.getResponseHeaders().set("Content-Type", "text/event-stream");
    exchange.sendResponseHeaders(200, 0);
    OutputStream out = exchange.getResponseBody();
    open.add(out);
    out.write(
        ("event: connected\ndata: " + served.etag() + "\n\n").getBytes(StandardCharsets.UTF_8));
    out.flush();
    released.await();
  }

  @Override
  public void close() {
    released.countDown();
    server.stop(0);
  }
}
