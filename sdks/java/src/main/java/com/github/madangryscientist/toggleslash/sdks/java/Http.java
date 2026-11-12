package com.github.madangryscientist.toggleslash.sdks.java;

import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.net.http.HttpTimeoutException;
import java.time.Duration;
import java.util.Optional;
import java.util.Set;
import java.util.concurrent.CancellationException;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

/**
 * The SDK's HTTP: one {@link HttpClient} (HTTP/1.1, no redirects) and a bound on every whole
 * request, body included. {@code HttpRequest.timeout} ends at the response headers, so the bound is
 * a wait on the response future, which is cancelled when it passes.
 */
final class Http {
  /** A request that did not produce a response: {@code code} is the {@code lastError} value. */
  static final class Failure extends Exception {
    private static final long serialVersionUID = 1L;
    final String code;

    Failure(String code, String message, Throwable cause) {
      super(message, cause);
      this.code = code;
    }
  }

  private final Config config;
  private final HttpClient client;
  private final Set<CompletableFuture<?>> inFlight = ConcurrentHashMap.newKeySet();
  private volatile boolean abandoned;

  Http(Config config) {
    this.config = config;
    HttpClient.Builder builder =
        HttpClient.newBuilder()
            .version(HttpClient.Version.HTTP_1_1)
            .followRedirects(HttpClient.Redirect.NEVER)
            .connectTimeout(Duration.ofMillis(config.timeoutMs()));
    if (config.sslContext() != null) {
      builder.sslContext(config.sslContext());
    }
    this.client = builder.build();
  }

  HttpRequest.Builder request(String path) throws Failure {
    try {
      return HttpRequest.newBuilder(URI.create(config.baseUrl() + path))
          .header("Authorization", "Bearer " + config.sdkKey());
    } catch (RuntimeException e) {
      throw new Failure("network", "the request could not be built", e);
    }
  }

  /**
   * Sends one request and waits at most the request timeout for the whole response.
   *
   * @param abandonable whether {@link #abandon()} cancels it (the ruleset and the stream, not
   *     usage)
   */
  <T> HttpResponse<T> send(
      HttpRequest request, HttpResponse.BodyHandler<T> handler, boolean abandonable)
      throws Failure {
    CompletableFuture<HttpResponse<T>> future;
    try {
      future = client.sendAsync(request, handler);
    } catch (RuntimeException e) {
      throw new Failure("network", "the request could not be sent", e);
    }
    if (abandonable) {
      inFlight.add(future);
      if (abandoned) {
        future.cancel(true);
      }
    }
    try {
      return future.get(config.timeoutMs(), TimeUnit.MILLISECONDS);
    } catch (TimeoutException e) {
      future.cancel(true);
      throw new Failure("timeout", "the request timed out", e);
    } catch (CancellationException e) {
      throw new Failure("network", "the request was cancelled", e);
    } catch (ExecutionException e) {
      throw new Failure(classify(e.getCause()), "the request failed", e.getCause());
    } catch (InterruptedException e) {
      future.cancel(true);
      Thread.currentThread().interrupt();
      throw new Failure("network", "the request was interrupted", e);
    } finally {
      inFlight.remove(future);
    }
  }

  private static String classify(Throwable cause) {
    for (Throwable t = cause; t != null; t = t.getCause()) {
      if (t instanceof HttpTimeoutException) {
        return "timeout";
      }
      if (t instanceof IOException) {
        break;
      }
    }
    return "network";
  }

  /** Cancels every abandonable request in flight, and any started later. */
  void abandon() {
    abandoned = true;
    for (CompletableFuture<?> future : inFlight) {
      future.cancel(true);
    }
  }

  /**
   * The wait a 429 asked for, in milliseconds, capped at 300 s; -1 for anything else. Only a 429
   * counts, and only whole decimal seconds (spaces around are ignored).
   */
  static long retryAfterMs(HttpResponse<?> response) {
    if (response.statusCode() != 429) {
      return -1;
    }
    Optional<String> header = response.headers().firstValue("Retry-After");
    if (header.isEmpty()) {
      return -1;
    }
    String text = header.get().strip();
    if (text.isEmpty()) {
      return -1;
    }
    for (int i = 0; i < text.length(); i++) {
      char c = text.charAt(i);
      if (c < '0' || c > '9') {
        return -1;
      }
    }
    int start = 0;
    while (start < text.length() - 1 && text.charAt(start) == '0') {
      start++;
    }
    String digits = text.substring(start);
    if (digits.length() > 6) {
      return Hold.CAP_MS;
    }
    return Math.min(Long.parseLong(digits) * 1000, Hold.CAP_MS);
  }
}
