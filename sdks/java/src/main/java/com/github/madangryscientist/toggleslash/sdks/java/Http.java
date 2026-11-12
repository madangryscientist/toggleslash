package com.github.madangryscientist.toggleslash.sdks.java;

import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.time.Duration;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.TimeoutException;

/**
 * The requests of one side of the SDK (the sync loop, or the usage flushes). It tracks the
 * exchanges in flight so they can be aborted, and once {@link #seal()}ed it refuses new ones.
 */
final class Http {
  private final HttpClient client;
  private final String base;
  private final String authorization;
  private final long timeoutNanos;
  private final Set<Exchange> active = ConcurrentHashMap.newKeySet();
  private volatile boolean sealed;

  Http(HttpClient client, Options options) {
    this.client = client;
    this.base = options.url();
    this.authorization = "Bearer " + options.sdkKey();
    this.timeoutNanos = options.timeoutNanos();
  }

  /**
   * The one client a {@link Client} shares between its two sides: HTTP/1.1, no redirects, no proxy
   * and no cookies.
   *
   * @param options the options
   * @return the client
   */
  static HttpClient newClient(Options options) {
    HttpClient.Builder builder =
        HttpClient.newBuilder()
            .version(HttpClient.Version.HTTP_1_1)
            .followRedirects(HttpClient.Redirect.NEVER)
            .proxy(HttpClient.Builder.NO_PROXY)
            .connectTimeout(Duration.ofNanos(options.timeoutNanos()));
    if (options.sslContext() != null) {
      builder.sslContext(options.sslContext());
    }
    return builder.build();
  }

  /** A request, started. The headers are here when it returns; the caller reads the body. */
  Exchange get(String path, String accept, String ifNoneMatch)
      throws IOException, TimeoutException, InterruptedException {
    HttpRequest.Builder builder = builder(path).GET();
    if (accept != null) {
      builder.header("Accept", accept);
    }
    if (ifNoneMatch != null) {
      builder.header("If-None-Match", ifNoneMatch);
    }
    return open(builder.build());
  }

  Exchange post(String path, byte[] body)
      throws IOException, TimeoutException, InterruptedException {
    HttpRequest request =
        builder(path)
            .header("Content-Type", "application/json")
            .POST(HttpRequest.BodyPublishers.ofByteArray(body))
            .build();
    return open(request);
  }

  private HttpRequest.Builder builder(String path) throws IOException {
    try {
      return HttpRequest.newBuilder(URI.create(base + path))
          .timeout(Duration.ofNanos(timeoutNanos))
          .header("Authorization", authorization);
    } catch (RuntimeException e) {
      throw new IOException("the URL or the key cannot be used", e);
    }
  }

  private Exchange open(HttpRequest request)
      throws IOException, TimeoutException, InterruptedException {
    Exchange exchange = new Exchange();
    active.add(exchange);
    if (sealed) {
      exchange.abort();
    }
    try {
      exchange.start(client, request, System.nanoTime() + timeoutNanos);
      return exchange;
    } catch (IOException | TimeoutException | InterruptedException | RuntimeException e) {
      release(exchange);
      throw e;
    }
  }

  /**
   * Finishes with an exchange: aborts it (freeing the connection) and forgets it.
   *
   * @param exchange the exchange, or null
   */
  void release(Exchange exchange) {
    if (exchange != null) {
      exchange.abort();
      active.remove(exchange);
    }
  }

  /** Aborts every exchange in flight. */
  void abortAll() {
    for (Exchange exchange : active) {
      exchange.abort();
    }
  }

  /** Aborts every exchange in flight and refuses new ones. */
  void seal() {
    sealed = true;
    abortAll();
  }
}
