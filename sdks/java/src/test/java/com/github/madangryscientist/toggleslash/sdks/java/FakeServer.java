package com.github.madangryscientist.toggleslash.sdks.java;

import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.TreeMap;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.Executors;
import java.util.function.Supplier;

/** A loopback ToggleSlash server for the SDK's own tests: it records every request it gets. */
final class FakeServer implements AutoCloseable {
  /** One request as the server saw it. */
  record Seen(String method, String path, Map<String, List<String>> headers, String body) {}

  @FunctionalInterface
  interface Handler {
    void handle(HttpExchange exchange) throws Exception;
  }

  static final String RULESET =
      "{\"flags\":[{\"key\":\"f\",\"variations\":[false,true],\"on\":true,\"offVariation\":0,"
          + "\"targets\":[],\"prerequisites\":[],\"rules\":[],\"fallthrough\":{\"variation\":1},"
          + "\"salt\":\"s\",\"version\":7}],\"segments\":[]}";

  final List<Seen> seen = new CopyOnWriteArrayList<>();

  /** Counted down at close, so a handler that hangs can be let go. */
  final CountDownLatch released = new CountDownLatch(1);

  volatile Handler ruleset = ok(() -> RULESET, "\"v1\"");
  volatile Handler stream = status(404);
  volatile Handler events = status(204);

  private final HttpServer server;

  FakeServer() throws IOException {
    this(HttpServer.create(new InetSocketAddress(InetAddress.getLoopbackAddress(), 0), 0));
  }

  FakeServer(HttpServer server) {
    this.server = server;
    server.setExecutor(
        Executors.newCachedThreadPool(r -> {
          Thread t = new Thread(r, "fake-server");
          t.setDaemon(true);
          return t;
        }));
    server.createContext("/sdk/v1/ruleset", ex -> serve(ex, ruleset));
    server.createContext("/sdk/v1/stream", ex -> serve(ex, stream));
    server.createContext("/sdk/v1/events", ex -> serve(ex, events));
    server.start();
  }

  String url() {
    return (server instanceof com.sun.net.httpserver.HttpsServer ? "https" : "http")
        + "://127.0.0.1:"
        + server.getAddress().getPort();
  }

  List<Seen> to(String path) {
    List<Seen> out = new ArrayList<>();
    for (Seen s : seen) {
      if (s.path().equals(path)) {
        out.add(s);
      }
    }
    return out;
  }

  private void serve(HttpExchange exchange, Handler handler) throws IOException {
    Map<String, List<String>> headers = new TreeMap<>();
    exchange.getRequestHeaders().forEach((k, v) -> headers.put(k.toLowerCase(), List.copyOf(v)));
    String body = new String(exchange.getRequestBody().readAllBytes(), StandardCharsets.UTF_8);
    seen.add(
        new Seen(exchange.getRequestMethod(), exchange.getRequestURI().getPath(), headers, body));
    try {
      handler.handle(exchange);
    } catch (IOException e) {
      // The client went away.
    } catch (Exception e) {
      exchange.close();
    }
  }

  static Handler ok(Supplier<String> body, String etag) {
    return ex -> {
      if (etag != null) {
        ex.getResponseHeaders().set("ETag", etag);
      }
      ex.getResponseHeaders().set("Content-Type", "application/json");
      send(ex, 200, body.get());
    };
  }

  static Handler status(int code) {
    return ex -> send(ex, code, "");
  }

  static Handler tooMany(String retryAfter) {
    return ex -> {
      ex.getResponseHeaders().set("Retry-After", retryAfter);
      send(ex, 429, "");
    };
  }

  /** Holds the request open until the server closes. */
  Handler hang() {
    return ex -> {
      try {
        released.await();
      } catch (InterruptedException e) {
        Thread.currentThread().interrupt();
      }
    };
  }

  static void send(HttpExchange ex, int code, String body) throws IOException {
    byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
    ex.sendResponseHeaders(code, bytes.length == 0 ? -1 : bytes.length);
    if (bytes.length > 0) {
      try (OutputStream out = ex.getResponseBody()) {
        out.write(bytes);
      }
    } else {
      ex.close();
    }
  }

  /** Opens an event stream: headers go out now, and the returned stream takes the events. */
  static OutputStream openStream(HttpExchange ex) throws IOException {
    ex.getResponseHeaders().set("Content-Type", "text/event-stream");
    ex.sendResponseHeaders(200, 0);
    OutputStream out = ex.getResponseBody();
    out.flush();
    return out;
  }

  static void write(OutputStream out, String text) throws IOException {
    out.write(text.getBytes(StandardCharsets.UTF_8));
    out.flush();
  }

  @Override
  public void close() {
    released.countDown();
    server.stop(0);
  }
}
