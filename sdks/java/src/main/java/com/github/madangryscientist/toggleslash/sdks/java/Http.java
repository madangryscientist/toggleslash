package com.github.madangryscientist.toggleslash.sdks.java;

import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpHeaders;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.net.http.HttpTimeoutException;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.Set;
import java.util.concurrent.CancellationException;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

/**
 * The three requests the SDK makes, over the JDK's HTTP client (HTTP/1.1, redirects off). A request
 * is bounded as a whole: the caller waits on the future for what is left of the time and cancels
 * the exchange when it runs out, since the client's own request timeout ends at the response
 * headers. The JDK before 21 has no closable client, so the requests that follow the server
 * register a canceller, and {@link #abandon()} runs them.
 */
final class Http {
  /** Why a request did not produce an answer. */
  enum Kind {
    TIMEOUT,
    NETWORK,
    ABORTED
  }

  /** A request without an answer. */
  static final class Failure extends Exception {
    private static final long serialVersionUID = 1L;

    private final transient Kind kind;

    Failure(Kind kind) {
      super(kind.name(), null, false, false);
      this.kind = kind;
    }

    Kind kind() {
      return kind;
    }
  }

  /**
   * A complete answer; {@code body} is empty unless the status was 200 and a body was asked for.
   */
  record Response(int status, HttpHeaders headers, byte[] body) {}

  /** An open stream answer. */
  record Opened(int status, HttpHeaders headers, StreamBody body) {}

  private final HttpClient client;
  private final String base;
  private final String key;
  private final Set<Runnable> following = ConcurrentHashMap.newKeySet();
  private volatile boolean abandoned;

  Http(Options options) {
    HttpClient.Builder builder =
        HttpClient.newBuilder()
            .version(HttpClient.Version.HTTP_1_1)
            .followRedirects(HttpClient.Redirect.NEVER)
            .connectTimeout(Duration.ofNanos(options.requestNanos()));
    if (options.sslContext() != null) {
      builder.sslContext(options.sslContext());
    }
    this.client = builder.build();
    this.base = options.url();
    this.key = options.sdkKey();
  }

  private HttpRequest.Builder request(String path, Duration timeout) {
    return HttpRequest.newBuilder(URI.create(base + path))
        .timeout(timeout)
        .header("Authorization", "Bearer " + key);
  }

  /** The ruleset request. A 200 body is read; any other answer's body is dropped. */
  Response getRuleset(String ifNoneMatch, long timeoutNanos) throws Failure {
    Duration timeout = Duration.ofNanos(timeoutNanos);
    HttpRequest request;
    try {
      HttpRequest.Builder builder = request("/sdk/v1/ruleset", timeout);
      if (ifNoneMatch != null) {
        builder.header("If-None-Match", ifNoneMatch);
      }
      request = builder.GET().build();
    } catch (RuntimeException unusable) {
      throw new Failure(Kind.NETWORK);
    }
    HttpResponse<byte[]> response =
        await(
            request,
            info ->
                info.statusCode() == 200
                    ? HttpResponse.BodySubscribers.ofByteArray()
                    : HttpResponse.BodySubscribers.replacing(new byte[0]),
            timeoutNanos,
            true);
    return new Response(response.statusCode(), response.headers(), response.body());
  }

  /**
   * Opens the stream; the answer arrives with the headers, the body is read through {@link
   * StreamBody}.
   */
  Opened openStream(long timeoutNanos) throws Failure {
    Duration timeout = Duration.ofNanos(timeoutNanos);
    HttpRequest request;
    try {
      request =
          request("/sdk/v1/stream", timeout).header("Accept", "text/event-stream").GET().build();
    } catch (RuntimeException unusable) {
      throw new Failure(Kind.NETWORK);
    }
    HttpResponse<StreamBody> response =
        await(request, info -> new StreamBody(), timeoutNanos, true);
    StreamBody body = response.body();
    following.add(body.canceller);
    if (abandoned) {
      body.cancel();
    }
    return new Opened(response.statusCode(), response.headers(), body);
  }

  /** Ends the stream's registration; call it when the stream is done with. */
  void release(StreamBody body) {
    body.cancel();
    following.remove(body.canceller);
  }

  /** The events request. The status and headers count; the body is dropped. */
  Response postEvents(String json, long timeoutNanos) throws Failure {
    Duration timeout = Duration.ofNanos(timeoutNanos);
    HttpRequest request;
    try {
      request =
          request("/sdk/v1/events", timeout)
              .header("Content-Type", "application/json")
              .POST(HttpRequest.BodyPublishers.ofString(json, StandardCharsets.UTF_8))
              .build();
    } catch (RuntimeException unusable) {
      throw new Failure(Kind.NETWORK);
    }
    HttpResponse<byte[]> response =
        await(
            request,
            info -> HttpResponse.BodySubscribers.replacing(new byte[0]),
            timeoutNanos,
            false);
    return new Response(response.statusCode(), response.headers(), response.body());
  }

  /** Cancels every request and stream that follows the server, and every one that starts later. */
  void abandon() {
    abandoned = true;
    for (Runnable canceller : following) {
      canceller.run();
    }
  }

  private <T> HttpResponse<T> await(
      HttpRequest request, HttpResponse.BodyHandler<T> handler, long timeoutNanos, boolean follow)
      throws Failure {
    long deadline = System.nanoTime() + timeoutNanos;
    CompletableFuture<HttpResponse<T>> future;
    try {
      future = client.sendAsync(request, handler);
    } catch (RuntimeException unusable) {
      throw new Failure(Kind.NETWORK);
    }
    Runnable canceller = () -> future.cancel(true);
    if (follow) {
      following.add(canceller);
      if (abandoned) {
        future.cancel(true);
      }
    }
    try {
      return future.get(Math.max(0, deadline - System.nanoTime()), TimeUnit.NANOSECONDS);
    } catch (TimeoutException late) {
      future.cancel(true);
      throw new Failure(abandoned ? Kind.ABORTED : Kind.TIMEOUT);
    } catch (CancellationException cancelled) {
      throw new Failure(Kind.ABORTED);
    } catch (InterruptedException interrupted) {
      future.cancel(true);
      throw new Failure(Kind.ABORTED);
    } catch (ExecutionException failed) {
      throw new Failure(classify(failed.getCause()));
    } finally {
      if (follow) {
        following.remove(canceller);
      }
    }
  }

  private Kind classify(Throwable cause) {
    if (cause instanceof CancellationException) {
      return Kind.ABORTED;
    }
    if (abandoned && cause instanceof IOException) {
      return Kind.ABORTED;
    }
    return cause instanceof HttpTimeoutException ? Kind.TIMEOUT : Kind.NETWORK;
  }
}
