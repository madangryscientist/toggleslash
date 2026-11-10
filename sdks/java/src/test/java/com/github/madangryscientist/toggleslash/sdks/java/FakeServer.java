package com.github.madangryscientist.toggleslash.sdks.java;

import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

/** A ToggleSlash server on a real loopback socket, answering what each test tells it to. */
final class FakeServer implements AutoCloseable {
  /** What one route does with one request. */
  @FunctionalInterface
  interface Handler {
    void handle(HttpExchange exchange) throws Exception;
  }

  /** A request the server saw. */
  record Seen(String method, String path, Map<String, List<String>> headers, String body) {}

  static final String FLAG_TEMPLATE =
      "{\"key\":\"%s\",\"variations\":[%s,%s],\"on\":true,\"offVariation\":0,\"targets\":[],"
          + "\"prerequisites\":[],\"rules\":[],\"fallthrough\":{\"variation\":0},\"salt\":\"s\","
          + "\"version\":%d}";

  /** A flag that serves {@code value} (JSON text) to everyone, as variation 0. */
  static String flag(String key, String value, int version) {
    return String.format(FLAG_TEMPLATE, key, value, "null", version);
  }

  static String ruleset(String... flags) {
    return "{\"flags\":[" + String.join(",", flags) + "],\"segments\":[]}";
  }

  private final HttpServer server;
  private final ExecutorService executor = Executors.newCachedThreadPool();
  private final Map<String, Handler> routes = new ConcurrentHashMap<>();
  private final List<Seen> seen = Collections.synchronizedList(new ArrayList<>());

  /** Released at close, so a handler that hangs on purpose lets go. */
  final CountDownLatch released = new CountDownLatch(1);

  FakeServer() throws IOException {
    server = HttpServer.create(new InetSocketAddress(InetAddress.getLoopbackAddress(), 0), 0);
    server.setExecutor(executor);
    server.createContext(
        "/",
        exchange -> {
          byte[] body = exchange.getRequestBody().readAllBytes();
          seen.add(
              new Seen(
                  exchange.getRequestMethod(),
                  exchange.getRequestURI().getPath(),
                  Map.copyOf(exchange.getRequestHeaders()),
                  new String(body, StandardCharsets.UTF_8)));
          Handler handler = routes.get(exchange.getRequestURI().getPath());
          try {
            if (handler == null) {
              send(exchange, 404, "");
            } else {
              handler.handle(exchange);
            }
          } catch (IOException e) {
            // the client went away
          } catch (Exception e) {
            exchange.close();
          }
        });
    server.start();
  }

  String url() {
    return "http://127.0.0.1:" + server.getAddress().getPort();
  }

  FakeServer on(String path, Handler handler) {
    routes.put(path, handler);
    return this;
  }

  /** The ruleset route answers {@code body} with the ETag {@code etag}, whatever is asked. */
  FakeServer ruleset(String body, String etag) {
    return on(
        "/sdk/v1/ruleset",
        x -> {
          x.getResponseHeaders().set("ETag", etag);
          send(x, 200, body);
        });
  }

  FakeServer events(int status) {
    return on("/sdk/v1/events", x -> send(x, status, "{}"));
  }

  List<Seen> requests(String path) {
    synchronized (seen) {
      return seen.stream().filter(s -> s.path().equals(path)).toList();
    }
  }

  List<Seen> all() {
    synchronized (seen) {
      return new ArrayList<>(seen);
    }
  }

  static void send(HttpExchange exchange, int status, String body) throws IOException {
    byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
    if (!exchange.getResponseHeaders().containsKey("Content-Type")) {
      exchange.getResponseHeaders().set("Content-Type", "application/json");
    }
    exchange.sendResponseHeaders(status, bytes.length == 0 ? -1 : bytes.length);
    if (bytes.length > 0) {
      exchange.getResponseBody().write(bytes);
    }
    exchange.close();
  }

  /** Holds the request until the server closes. */
  void hang(HttpExchange exchange) throws InterruptedException, IOException {
    released.await();
    exchange.close();
  }

  @Override
  public void close() {
    released.countDown();
    server.stop(0);
    executor.shutdownNow();
  }
}
