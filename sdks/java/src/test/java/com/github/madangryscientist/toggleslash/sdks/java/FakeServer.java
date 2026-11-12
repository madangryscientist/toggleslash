package com.github.madangryscientist.toggleslash.sdks.java;

import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

/** A loopback HTTP server that records every request and answers as each test scripts it. */
final class FakeServer implements AutoCloseable {
  /** One request as the server saw it. */
  record Seen(String method, String path, Map<String, List<String>> headers, String body) {}

  /** Answers one request. */
  @FunctionalInterface
  interface Handler {
    void handle(HttpExchange exchange) throws Exception;
  }

  final List<Seen> seen = new CopyOnWriteArrayList<>();
  private final HttpServer server;
  private final ExecutorService pool =
      Executors.newCachedThreadPool(runnable -> {
        Thread t = new Thread(runnable, "fake-server");
        t.setDaemon(true);
        return t;
      });

  FakeServer() throws IOException {
    server = HttpServer.create(new InetSocketAddress(InetAddress.getLoopbackAddress(), 0), 128);
    server.setExecutor(pool);
    server.start();
  }

  String url() {
    return "http://127.0.0.1:" + server.getAddress().getPort();
  }

  /** Scripts a path. */
  void on(String path, Handler handler) {
    server.createContext(
        path,
        exchange -> {
          try {
            byte[] raw = exchange.getRequestBody().readAllBytes();
            seen.add(
                new Seen(
                    exchange.getRequestMethod(),
                    exchange.getRequestURI().getPath(),
                    new LinkedHashMap<>(exchange.getRequestHeaders()),
                    new String(raw, StandardCharsets.UTF_8)));
            handler.handle(exchange);
          } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
          } catch (Exception e) {
            // The client went away, which is what several tests do on purpose.
          } finally {
            exchange.close();
          }
        });
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

  /** Answers with a body and optional header pairs. */
  static void reply(HttpExchange exchange, int status, String body, String... headers)
      throws IOException {
    for (int i = 0; i + 1 < headers.length; i += 2) {
      exchange.getResponseHeaders().add(headers[i], headers[i + 1]);
    }
    byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
    if (status == 304 || bytes.length == 0) {
      exchange.sendResponseHeaders(status, -1);
      return;
    }
    exchange.sendResponseHeaders(status, bytes.length);
    try (OutputStream out = exchange.getResponseBody()) {
      out.write(bytes);
    }
  }

  /** Opens a chunked response and returns its body for the test to write to. */
  static OutputStream open(HttpExchange exchange) throws IOException {
    exchange.getResponseHeaders().add("Content-Type", "text/event-stream");
    exchange.sendResponseHeaders(200, 0);
    return exchange.getResponseBody();
  }

  /** Never answers (until the server closes). */
  static void hang() throws InterruptedException {
    Thread.sleep(60_000);
  }

  static void write(OutputStream out, String text) throws IOException {
    out.write(text.getBytes(StandardCharsets.UTF_8));
    out.flush();
  }

  /** A flag whose fallthrough serves the last of its variations. */
  static String flag(String key, int version, String... variations) {
    return "{\"key\":\""
        + key
        + "\",\"variations\":["
        + String.join(",", variations)
        + "],\"on\":true,\"offVariation\":0,\"targets\":[],\"prerequisites\":[],\"rules\":[],"
        + "\"fallthrough\":{\"variation\":"
        + (variations.length - 1)
        + "},\"salt\":\"s\",\"version\":"
        + version
        + "}";
  }

  static String ruleset(String... flags) {
    return "{\"flags\":[" + String.join(",", flags) + "],\"segments\":[]}";
  }

  @Override
  public void close() {
    server.stop(0);
    pool.shutdownNow();
  }
}
