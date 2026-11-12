package com.github.madangryscientist.toggleslash.sdks.java;

import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.TreeMap;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;

/**
 * A loopback ToggleSlash server for the native tests: records every request, scripted per route.
 */
final class FakeServer implements AutoCloseable {
  /** One request as the server saw it. */
  record Req(String method, String path, Map<String, String> headers, String body) {
    String header(String name) {
      return headers.get(name.toLowerCase(Locale.ROOT));
    }
  }

  /** Answers one request. */
  @FunctionalInterface
  interface Handler {
    void handle(HttpExchange exchange, Req request) throws Exception;
  }

  static final String KEY = "sdk-test-key";

  private final HttpServer server;
  private final ExecutorService executor = Executors.newCachedThreadPool();
  private final CountDownLatch released = new CountDownLatch(1);
  final List<Req> requests = Collections.synchronizedList(new ArrayList<>());
  volatile Handler ruleset = (exchange, request) -> reply(exchange, 404, "");
  volatile Handler stream = (exchange, request) -> reply(exchange, 404, "");
  volatile Handler events = (exchange, request) -> reply(exchange, 200, "{}");

  FakeServer() {
    this(null);
  }

  /** A server over TLS when a context is given. */
  FakeServer(javax.net.ssl.SSLContext tls) {
    try {
      if (tls == null) {
        server = HttpServer.create(new InetSocketAddress(InetAddress.getLoopbackAddress(), 0), 0);
      } else {
        com.sun.net.httpserver.HttpsServer https =
            com.sun.net.httpserver.HttpsServer.create(
                new InetSocketAddress(InetAddress.getLoopbackAddress(), 0), 0);
        https.setHttpsConfigurator(new com.sun.net.httpserver.HttpsConfigurator(tls));
        server = https;
      }
    } catch (IOException e) {
      throw new IllegalStateException(e);
    }
    server.setExecutor(executor);
    server.createContext("/", this::route);
    server.start();
  }

  String url() {
    String scheme = server instanceof com.sun.net.httpserver.HttpsServer ? "https" : "http";
    return scheme + "://127.0.0.1:" + server.getAddress().getPort();
  }

  int port() {
    return server.getAddress().getPort();
  }

  private void route(HttpExchange exchange) throws IOException {
    Map<String, String> headers = new TreeMap<>();
    exchange
        .getRequestHeaders()
        .forEach(
            (name, values) -> headers.put(name.toLowerCase(Locale.ROOT), String.join(",", values)));
    String body;
    try (InputStream in = exchange.getRequestBody()) {
      body = new String(in.readAllBytes(), StandardCharsets.UTF_8);
    }
    Req request =
        new Req(exchange.getRequestMethod(), exchange.getRequestURI().getPath(), headers, body);
    requests.add(request);
    try {
      Handler handler;
      switch (request.path()) {
        case "/sdk/v1/ruleset":
          handler = ruleset;
          break;
        case "/sdk/v1/stream":
          handler = stream;
          break;
        case "/sdk/v1/events":
          handler = events;
          break;
        default:
          handler = (e, r) -> reply(e, 404, "");
          break;
      }
      handler.handle(exchange, request);
    } catch (Exception e) {
      // The client went away mid-answer, or the server is closing.
    } finally {
      exchange.close();
    }
  }

  /** Requests to one path, in arrival order. */
  List<Req> to(String path) {
    List<Req> found = new ArrayList<>();
    synchronized (requests) {
      for (Req request : requests) {
        if (request.path().equals(path)) {
          found.add(request);
        }
      }
    }
    return found;
  }

  static void reply(HttpExchange exchange, int status, String body, String... headers)
      throws IOException {
    for (int i = 0; i + 1 < headers.length; i += 2) {
      exchange.getResponseHeaders().add(headers[i], headers[i + 1]);
    }
    byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
    if (status == 304) {
      exchange.sendResponseHeaders(status, -1);
      return;
    }
    exchange.sendResponseHeaders(status, bytes.length == 0 ? -1 : bytes.length);
    if (bytes.length > 0) {
      try (OutputStream out = exchange.getResponseBody()) {
        out.write(bytes);
      }
    }
  }

  /** Serves the ruleset with the tag, answering 304 to a matching If-None-Match. */
  static Handler serving(String etag, String json) {
    return (exchange, request) -> {
      if (etag.equals(request.header("if-none-match"))) {
        reply(exchange, 304, "", "ETag", etag);
      } else {
        reply(exchange, 200, json, "ETag", etag, "Content-Type", "application/json");
      }
    };
  }

  /** Never answers until the server closes. */
  Handler hang() {
    return (exchange, request) -> released.await();
  }

  /** Sends the headers, then holds the response open until the server closes. */
  Handler hangBody() {
    return (exchange, request) -> {
      exchange.sendResponseHeaders(200, 0);
      exchange.getResponseBody().write(' ');
      exchange.getResponseBody().flush();
      released.await();
    };
  }

  /** Opens an event stream, writes the chunks 20 ms apart, then holds it open. */
  Handler sse(String... chunks) {
    return (exchange, request) -> {
      exchange.getResponseHeaders().add("Content-Type", "text/event-stream");
      exchange.sendResponseHeaders(200, 0);
      OutputStream out = exchange.getResponseBody();
      out.flush();
      for (String chunk : chunks) {
        out.write(chunk.getBytes(StandardCharsets.UTF_8));
        out.flush();
        Thread.sleep(20);
      }
      released.await();
    };
  }

  @Override
  public void close() {
    released.countDown();
    server.stop(0);
    executor.shutdownNow();
    try {
      executor.awaitTermination(2, TimeUnit.SECONDS);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
  }
}
