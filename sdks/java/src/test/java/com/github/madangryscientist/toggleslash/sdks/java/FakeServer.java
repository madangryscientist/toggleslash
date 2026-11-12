package com.github.madangryscientist.toggleslash.sdks.java;

import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import com.sun.net.httpserver.HttpsConfigurator;
import com.sun.net.httpserver.HttpsServer;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.Executors;
import javax.net.ssl.SSLContext;

/**
 * A loopback ToggleSlash server for the native tests. It records every request with the headers the
 * client sent, answers each route with the handler the test gave it, and can hold a request open
 * until the test (or {@link #close}) lets it go.
 */
final class FakeServer implements AutoCloseable {
  /** One request as the server saw it. */
  record Req(String method, String path, Map<String, String> headers, byte[] body, long at) {
    String text() {
      return new String(body, StandardCharsets.UTF_8);
    }
  }

  /** Answers one route. */
  interface Route {
    void handle(Req request, HttpExchange exchange) throws Exception;
  }

  private final HttpServer server;
  private final boolean tls;
  private final Map<String, Route> routes = new java.util.concurrent.ConcurrentHashMap<>();
  private final List<Req> requests = new CopyOnWriteArrayList<>();
  private final CountDownLatch released = new CountDownLatch(1);

  FakeServer() throws IOException {
    this(null);
  }

  FakeServer(SSLContext tlsContext) throws IOException {
    InetSocketAddress address = new InetSocketAddress(InetAddress.getByName("127.0.0.1"), 0);
    if (tlsContext == null) {
      server = HttpServer.create(address, 0);
      tls = false;
    } else {
      HttpsServer https = HttpsServer.create(address, 0);
      https.setHttpsConfigurator(new HttpsConfigurator(tlsContext));
      server = https;
      tls = true;
    }
    server.setExecutor(
        Executors.newCachedThreadPool(r -> {
          Thread t = new Thread(r, "fake-server");
          t.setDaemon(true);
          return t;
        }));
    server.createContext("/", this::dispatch);
    server.start();
  }

  String url() {
    return (tls ? "https" : "http") + "://127.0.0.1:" + server.getAddress().getPort();
  }

  FakeServer route(String path, Route route) {
    routes.put(path, route);
    return this;
  }

  List<Req> requests(String path) {
    List<Req> out = new ArrayList<>();
    for (Req r : requests) {
      if (r.path().equals(path)) {
        out.add(r);
      }
    }
    return out;
  }

  /** Holds the request open for a route, then lets it go when the server closes. */
  void hangFor() throws IOException {
    try {
      hang();
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
  }

  /** Blocks until the test calls {@link #close} (or the request's client gives up). */
  void hang() throws InterruptedException {
    released.await();
  }

  private void dispatch(HttpExchange exchange) throws IOException {
    byte[] body;
    try (InputStream in = exchange.getRequestBody()) {
      body = in.readAllBytes();
    }
    Map<String, String> headers = new LinkedHashMap<>();
    exchange
        .getRequestHeaders()
        .forEach(
            (name, values) -> headers.put(name.toLowerCase(java.util.Locale.ROOT), values.get(0)));
    Req req =
        new Req(
            exchange.getRequestMethod(),
            exchange.getRequestURI().getPath(),
            headers,
            body,
            System.currentTimeMillis());
    requests.add(req);
    Route route = routes.get(req.path());
    try {
      if (route == null) {
        reply(exchange, 404, Map.of(), "");
      } else {
        route.handle(req, exchange);
      }
    } catch (Exception e) {
      // the client went away, or the test is over: nothing to answer
    } finally {
      exchange.close();
    }
  }

  static void reply(HttpExchange exchange, int status, Map<String, String> headers, String body)
      throws IOException {
    headers.forEach((k, v) -> exchange.getResponseHeaders().set(k, v));
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

  /** Opens an event stream and returns the body to write chunks to. */
  static OutputStream openStream(HttpExchange exchange) throws IOException {
    exchange.getResponseHeaders().set("Content-Type", "text/event-stream");
    exchange.sendResponseHeaders(200, 0);
    return exchange.getResponseBody();
  }

  /**
   * A ruleset with one boolean flag {@code f} (variations false, true) and the given fallthrough.
   */
  static String ruleset(int fallthrough, long version) {
    return "{\"flags\":[{\"key\":\"f\",\"variations\":[false,true],\"on\":true,\"offVariation\":0,"
        + "\"targets\":[],\"prerequisites\":[],\"rules\":[],\"fallthrough\":{\"variation\":"
        + fallthrough
        + "},\"salt\":\"s\",\"version\":"
        + version
        + "}],\"segments\":[]}";
  }

  /** A ruleset with one flag {@code n} serving {@code literal} (any JSON) as its only variation. */
  static String valueRuleset(String literal) {
    return "{\"flags\":[{\"key\":\"n\",\"variations\":["
        + literal
        + "],\"on\":true,\"offVariation\":0,\"targets\":[],\"prerequisites\":[],\"rules\":[],"
        + "\"fallthrough\":{\"variation\":0},\"salt\":\"s\",\"version\":1}],\"segments\":[]}";
  }

  /** Serves {@code body} with an ETag, and a 304 to a request that names that tag. */
  static Route rulesetRoute(String etag, String body) {
    return (req, ex) -> {
      if (etag.equals(req.headers().get("if-none-match"))) {
        reply(ex, 304, Map.of("ETag", etag), "");
      } else {
        reply(ex, 200, Map.of("ETag", etag, "Content-Type", "application/json"), body);
      }
    };
  }

  @Override
  public void close() {
    released.countDown();
    server.stop(0);
  }
}
