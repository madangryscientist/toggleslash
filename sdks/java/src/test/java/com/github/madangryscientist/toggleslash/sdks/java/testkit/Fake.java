package com.github.madangryscientist.toggleslash.sdks.java.testkit;

import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.Executors;
import java.util.function.Predicate;

/** A loopback ToggleSlash server a test scripts, which records every request it gets. */
public final class Fake implements AutoCloseable {
  /** What the server saw. */
  public record Seen(
      String method, String path, Map<String, List<String>> headers, String body, long at) {
    /**
     * Reads one header, case-insensitively.
     *
     * @param name the header name
     * @return the first value or null
     */
    public String header(String name) {
      for (Map.Entry<String, List<String>> e : headers.entrySet()) {
        if (e.getKey().equalsIgnoreCase(name)) {
          return e.getValue().get(0);
        }
      }
      return null;
    }
  }

  /** Answers one request. */
  @FunctionalInterface
  public interface Handler {
    /**
     * Handles a request.
     *
     * @param x the exchange
     * @throws Exception anything; the server closes the connection
     */
    void handle(HttpExchange x) throws Exception;
  }

  /** Everything received, in order. */
  public final List<Seen> seen = new CopyOnWriteArrayList<>();

  /** The ruleset route. */
  public volatile Handler ruleset = x -> reply(x, 404, "{}");

  /** The stream route. */
  public volatile Handler stream = x -> reply(x, 404, "{}");

  /** The events route. */
  public volatile Handler events = x -> reply(x, 200, "{}");

  private final HttpServer server;

  /**
   * Starts a plain HTTP server on a free loopback port.
   *
   * @throws IOException if it cannot bind
   */
  public Fake() throws IOException {
    this(HttpServer.create(new InetSocketAddress(InetAddress.getLoopbackAddress(), 0), 0));
  }

  /**
   * Wraps a server the caller made (an HTTPS one, for example).
   *
   * @param server the server, not yet started
   */
  public Fake(HttpServer server) {
    this.server = server;
    server.createContext(
        "/",
        x -> {
          try {
            String body = new String(x.getRequestBody().readAllBytes(), StandardCharsets.UTF_8);
            seen.add(
                new Seen(
                    x.getRequestMethod(),
                    x.getRequestURI().getPath(),
                    Map.copyOf(x.getRequestHeaders()),
                    body,
                    System.currentTimeMillis()));
            String path = x.getRequestURI().getPath();
            Handler handler =
                switch (path) {
                  case "/sdk/v1/ruleset" -> ruleset;
                  case "/sdk/v1/stream" -> stream;
                  case "/sdk/v1/events" -> events;
                  default -> y -> reply(y, 404, "{}");
                };
            handler.handle(x);
          } catch (Exception e) {
            x.close();
          }
        });
    server.setExecutor(
        Executors.newCachedThreadPool(r -> {
          Thread t = new Thread(r, "fake-server");
          t.setDaemon(true);
          return t;
        }));
    server.start();
  }

  /**
   * The base URL.
   *
   * @return {@code http://127.0.0.1:<port>}
   */
  public String url() {
    String scheme = server instanceof com.sun.net.httpserver.HttpsServer ? "https" : "http";
    return scheme + "://127.0.0.1:" + server.getAddress().getPort();
  }

  /**
   * Counts the requests seen on a path.
   *
   * @param path the path
   * @return the count
   */
  public long count(String path) {
    return seen.stream().filter(s -> s.path().equals(path)).count();
  }

  /**
   * The requests seen on a path.
   *
   * @param path the path
   * @return them in order
   */
  public List<Seen> on(String path) {
    return seen.stream().filter(s -> s.path().equals(path)).toList();
  }

  /**
   * Waits until a condition holds on the requests seen.
   *
   * @param millis how long to wait at most
   * @param condition the condition
   * @return whether it held
   */
  public boolean await(long millis, Predicate<List<Seen>> condition) {
    long end = System.nanoTime() + millis * 1_000_000L;
    while (System.nanoTime() < end) {
      if (condition.test(seen)) {
        return true;
      }
      try {
        Thread.sleep(5);
      } catch (InterruptedException e) {
        Thread.currentThread().interrupt();
        return false;
      }
    }
    return condition.test(seen);
  }

  /**
   * Answers a request with a JSON body.
   *
   * @param x the exchange
   * @param status the status
   * @param body the body
   * @throws IOException on a write failure
   */
  public static void reply(HttpExchange x, int status, String body) throws IOException {
    byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
    x.getResponseHeaders().set("Content-Type", "application/json");
    x.sendResponseHeaders(status, bytes.length == 0 ? -1 : bytes.length);
    if (bytes.length > 0) {
      try (OutputStream out = x.getResponseBody()) {
        out.write(bytes);
      }
    }
    x.close();
  }

  /**
   * Answers a ruleset request with a body and an ETag.
   *
   * @param x the exchange
   * @param etag the ETag, quoted
   * @param body the body
   * @throws IOException on a write failure
   */
  public static void rulesetReply(HttpExchange x, String etag, String body) throws IOException {
    x.getResponseHeaders().set("ETag", etag);
    reply(x, 200, body);
  }

  /**
   * A flag that is on, serves its fallthrough, and has two variations.
   *
   * @param key the key
   * @param version the flag's version
   * @param variations the variations as JSON text, two of them
   * @param fallthrough the fallthrough variation's index
   * @return the flag as JSON text
   */
  public static String flag(String key, long version, String variations, int fallthrough) {
    return "{\"key\":\""
        + key
        + "\",\"variations\":"
        + variations
        + ",\"on\":true,\"offVariation\":0,\"targets\":[],\"prerequisites\":[],\"rules\":[],"
        + "\"fallthrough\":{\"variation\":"
        + fallthrough
        + "},\"salt\":\"s\",\"version\":"
        + version
        + "}";
  }

  /**
   * A ruleset body.
   *
   * @param flags the flags as JSON text
   * @return the body
   */
  public static String body(String... flags) {
    return "{\"flags\":[" + String.join(",", flags) + "],\"segments\":[]}";
  }

  @Override
  public void close() {
    server.stop(0);
  }
}
