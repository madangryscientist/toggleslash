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
import java.util.List;
import java.util.Map;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.Executors;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import javax.net.ssl.SSLContext;

/**
 * A loopback ToggleSlash server for the package's own tests: the three routes the SDK may call,
 * each replaceable, every request recorded with its headers and body.
 */
final class FakeServer implements AutoCloseable {

  /** One request as the server saw it. */
  record Seen(String method, String path, Map<String, List<String>> headers, byte[] body) {

    String header(String name) {
      for (Map.Entry<String, List<String>> e : headers.entrySet()) {
        if (e.getKey().equalsIgnoreCase(name)) {
          return e.getValue().get(0);
        }
      }
      return null;
    }

    String text() {
      return new String(body, StandardCharsets.UTF_8);
    }
  }

  /** What a route does with one request. */
  interface Handler {
    void handle(HttpExchange exchange) throws IOException, InterruptedException;
  }

  static final String KEY = "sdk-test-key-0000";

  private final HttpServer server;
  private final String scheme;
  final List<Seen> ruleset = new CopyOnWriteArrayList<>();
  final List<Seen> stream = new CopyOnWriteArrayList<>();
  final List<Seen> events = new CopyOnWriteArrayList<>();
  private final BlockingQueue<String> streamQueue = new LinkedBlockingQueue<>();
  final AtomicInteger streamOpen = new AtomicInteger();

  volatile String rulesetBody = rulesetOf();
  volatile String etag = "\"v1\"";
  volatile Handler rulesetHandler = this::defaultRuleset;
  volatile Handler streamHandler = this::defaultStream;
  volatile Handler eventsHandler = x -> reply(x, 204, "");

  FakeServer() {
    this(null);
  }

  FakeServer(SSLContext tls) {
    try {
      InetSocketAddress at = new InetSocketAddress(InetAddress.getLoopbackAddress(), 0);
      if (tls == null) {
        server = HttpServer.create(at, 0);
        scheme = "http";
      } else {
        HttpsServer https = HttpsServer.create(at, 0);
        https.setHttpsConfigurator(new HttpsConfigurator(tls));
        server = https;
        scheme = "https";
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
    route("/sdk/v1/ruleset", ruleset, () -> rulesetHandler);
    route("/sdk/v1/stream", stream, () -> streamHandler);
    route("/sdk/v1/events", events, () -> eventsHandler);
    server.start();
  }

  private void route(String path, List<Seen> log, java.util.function.Supplier<Handler> handler) {
    server.createContext(
        path,
        exchange -> {
          try {
            byte[] body = exchange.getRequestBody().readAllBytes();
            log.add(
                new Seen(
                    exchange.getRequestMethod(),
                    exchange.getRequestURI().getPath(),
                    Map.copyOf(exchange.getRequestHeaders()),
                    body));
            handler.get().handle(exchange);
          } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
          } catch (IOException | RuntimeException e) {
            // a client that went away is not the server's problem
          } finally {
            exchange.close();
          }
        });
  }

  String url() {
    return scheme + "://127.0.0.1:" + server.getAddress().getPort();
  }

  /** Replaces the served ruleset; the tag changes with it. */
  void serve(String body, String tag) {
    rulesetBody = body;
    etag = tag;
  }

  /** Writes a chunk to the open stream (see {@link #defaultStream}). */
  void push(String chunk) {
    streamQueue.add(chunk);
  }

  static void reply(HttpExchange x, int status, String body) throws IOException {
    byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
    if (bytes.length == 0) {
      x.sendResponseHeaders(status, -1);
    } else {
      x.sendResponseHeaders(status, bytes.length);
      try (OutputStream out = x.getResponseBody()) {
        out.write(bytes);
      }
    }
  }

  private void defaultRuleset(HttpExchange x) throws IOException {
    String tag = etag;
    if (tag.equals(x.getRequestHeaders().getFirst("If-None-Match"))) {
      x.getResponseHeaders().add("ETag", tag);
      reply(x, 304, "");
      return;
    }
    x.getResponseHeaders().add("ETag", tag);
    x.getResponseHeaders().add("Content-Type", "application/json");
    reply(x, 200, rulesetBody);
  }

  /** Sends {@code connected} with the held tag, then whatever {@link #push} queues; "END" ends. */
  private void defaultStream(HttpExchange x) throws IOException, InterruptedException {
    x.getResponseHeaders().add("Content-Type", "text/event-stream");
    x.sendResponseHeaders(200, 0);
    streamOpen.incrementAndGet();
    OutputStream out = x.getResponseBody();
    out.write(("event: connected\ndata: " + etag + "\n\n").getBytes(StandardCharsets.UTF_8));
    out.flush();
    while (true) {
      String chunk = streamQueue.poll(20, TimeUnit.SECONDS);
      if (chunk == null || chunk.equals("END")) {
        return;
      }
      out.write(chunk.getBytes(StandardCharsets.UTF_8));
      out.flush();
    }
  }

  // ---------------------------------------------------------------- ruleset JSON

  /** A flag with one variation (a JSON literal) that is always served. */
  static String flag(String key, String valueJson, long version) {
    return "{\"key\":\""
        + key
        + "\",\"variations\":["
        + valueJson
        + "],\"on\":true,\"offVariation\":0,\"targets\":[],\"prerequisites\":[],\"rules\":[],"
        + "\"fallthrough\":{\"variation\":0},\"salt\":\"s\",\"version\":"
        + version
        + "}";
  }

  /** A ruleset of the given flags (JSON objects) and no segments. */
  static String rulesetOf(String... flags) {
    return "{\"flags\":[" + String.join(",", flags) + "],\"segments\":[]}";
  }

  @Override
  public void close() {
    server.stop(0);
    ((java.util.concurrent.ExecutorService) server.getExecutor()).shutdownNow();
  }
}
