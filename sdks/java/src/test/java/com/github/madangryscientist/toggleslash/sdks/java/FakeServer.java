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
import java.util.Locale;
import java.util.Map;
import java.util.TreeMap;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.Executors;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;

/**
 * A ToggleSlash server on a loopback port for tests: it records every request with its headers,
 * answers the three routes from fields a test sets, and streams events a test queues.
 */
final class FakeServer implements AutoCloseable {
  /** One request as the server saw it. Header names are lowercase. */
  record Req(String method, String path, Map<String, List<String>> headers, String body) {
    String header(String name) {
      List<String> values = headers.get(name.toLowerCase(Locale.ROOT));
      return values == null ? null : values.get(0);
    }
  }

  /** Marks the end of a streamed body. */
  static final String END = "\u0000end";

  final List<Req> requests = new CopyOnWriteArrayList<>();

  volatile int rulesetStatus = 200;
  volatile String rulesetBody = ruleset();
  volatile String etag = "\"v1\"";
  volatile String rulesetRetryAfter;

  /** When set, the ruleset route waits for this before answering. */
  volatile CountDownLatch rulesetGate;

  volatile int streamStatus = 200;
  volatile String streamRetryAfter;
  final LinkedBlockingQueue<String> streamOut = new LinkedBlockingQueue<>();

  /** When true the stream answers 200, writes nothing and keeps the connection open. */
  volatile boolean streamQuiet;

  volatile int eventsStatus = 204;
  volatile String eventsRetryAfter;
  volatile CountDownLatch eventsGate;

  private final HttpServer server;
  private final CountDownLatch done = new CountDownLatch(1);

  FakeServer() throws IOException {
    server = HttpServer.create(new InetSocketAddress(InetAddress.getLoopbackAddress(), 0), 0);
    server.setExecutor(
        Executors.newCachedThreadPool(r -> {
          Thread t = new Thread(r, "fake-server");
          t.setDaemon(true);
          return t;
        }));
    server.createContext("/sdk/v1/ruleset", this::ruleset);
    server.createContext("/sdk/v1/stream", this::stream);
    server.createContext("/sdk/v1/events", this::events);
    server.createContext(
        "/",
        ex -> {
          record(ex);
          reply(ex, 404, "", null);
        });
    server.start();
  }

  String url() {
    return "http://127.0.0.1:" + server.getAddress().getPort();
  }

  int port() {
    return server.getAddress().getPort();
  }

  /** The requests to one path, in order. */
  List<Req> to(String path) {
    List<Req> out = new ArrayList<>();
    for (Req r : requests) {
      if (r.path().equals(path)) {
        out.add(r);
      }
    }
    return out;
  }

  int count(String path) {
    return to(path).size();
  }

  /** Waits until a path has been asked {@code n} times. */
  boolean await(String path, int n, long millis) throws InterruptedException {
    long end = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(millis);
    while (System.nanoTime() < end) {
      if (count(path) >= n) {
        return true;
      }
      Thread.sleep(5);
    }
    return count(path) >= n;
  }

  /** Changes the ETag and the body together, so a request never sees a mix. */
  synchronized void serve(String newEtag, String newBody) {
    this.etag = newEtag;
    this.rulesetBody = newBody;
  }

  /** A ruleset JSON document with the given flag objects. */
  static String ruleset(String... flags) {
    return "{\"flags\":[" + String.join(",", flags) + "],\"segments\":[]}";
  }

  /** A flag that is on and serves variation {@code serve} of {@code variations} to everyone. */
  static String flag(String key, long version, String variations, int serve) {
    return "{\"key\":\""
        + key
        + "\",\"variations\":"
        + variations
        + ",\"on\":true,\"offVariation\":0,\"targets\":[],\"prerequisites\":[],\"rules\":[],"
        + "\"fallthrough\":{\"variation\":"
        + serve
        + "},\"salt\":\"s\",\"version\":"
        + version
        + "}";
  }

  private Req record(HttpExchange ex) throws IOException {
    Map<String, List<String>> headers = new TreeMap<>();
    ex.getRequestHeaders()
        .forEach((k, v) -> headers.put(k.toLowerCase(Locale.ROOT), List.copyOf(v)));
    String body = new String(ex.getRequestBody().readAllBytes(), StandardCharsets.UTF_8);
    Req req = new Req(ex.getRequestMethod(), ex.getRequestURI().getPath(), headers, body);
    requests.add(req);
    return req;
  }

  private static void reply(HttpExchange ex, int status, String body, String retryAfter)
      throws IOException {
    byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
    if (retryAfter != null) {
      ex.getResponseHeaders().set("Retry-After", retryAfter);
    }
    ex.sendResponseHeaders(status, status == 204 || status == 304 ? -1 : bytes.length);
    if (status != 204 && status != 304) {
      try (OutputStream out = ex.getResponseBody()) {
        out.write(bytes);
      }
    }
    ex.close();
  }

  private void ruleset(HttpExchange ex) throws IOException {
    Req req = record(ex);
    CountDownLatch gate = rulesetGate;
    if (gate != null) {
      try {
        gate.await();
      } catch (InterruptedException e) {
        Thread.currentThread().interrupt();
      }
    }
    int status = rulesetStatus;
    if (status == 200) {
      String held;
      String body;
      synchronized (this) {
        held = etag;
        body = rulesetBody;
      }
      if (held != null && held.equals(req.header("if-none-match"))) {
        ex.getResponseHeaders().set("ETag", held);
        reply(ex, 304, "", null);
        return;
      }
      if (held != null) {
        ex.getResponseHeaders().set("ETag", held);
      }
      ex.getResponseHeaders().set("Content-Type", "application/json");
      reply(ex, 200, body, null);
      return;
    }
    reply(ex, status, "{}", rulesetRetryAfter);
  }

  private void events(HttpExchange ex) throws IOException {
    record(ex);
    CountDownLatch gate = eventsGate;
    if (gate != null) {
      try {
        gate.await();
      } catch (InterruptedException e) {
        Thread.currentThread().interrupt();
      }
    }
    reply(ex, eventsStatus, "", eventsRetryAfter);
  }

  private void stream(HttpExchange ex) throws IOException {
    record(ex);
    if (streamStatus != 200) {
      reply(ex, streamStatus, "", streamRetryAfter);
      return;
    }
    ex.getResponseHeaders().set("Content-Type", "text/event-stream");
    ex.sendResponseHeaders(200, 0);
    try (OutputStream out = ex.getResponseBody()) {
      out.flush();
      while (!streamQuiet) {
        String next = streamOut.poll(50, TimeUnit.MILLISECONDS);
        if (next == null) {
          if (done.getCount() == 0) {
            return;
          }
          continue;
        }
        if (next.equals(END)) {
          return;
        }
        out.write(next.getBytes(StandardCharsets.UTF_8));
        out.flush();
      }
      done.await();
    } catch (IOException | InterruptedException e) {
      // the client went away
    } finally {
      ex.close();
    }
  }

  /** Queues an event for the stream. */
  void event(String name, String data) {
    streamOut.add("event: " + name + "\ndata: " + data + "\n\n");
  }

  @Override
  public void close() {
    done.countDown();
    CountDownLatch g = rulesetGate;
    if (g != null) {
      g.countDown();
    }
    g = eventsGate;
    if (g != null) {
      g.countDown();
    }
    server.stop(0);
  }
}
