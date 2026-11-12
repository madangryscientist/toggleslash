package com.github.madangryscientist.toggleslash.sdks.java;

import java.io.IOException;
import java.io.InputStream;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.net.http.HttpResponse.BodyHandler;
import java.net.http.HttpResponse.BodyHandlers;
import java.net.http.HttpResponse.BodySubscriber;
import java.net.http.HttpTimeoutException;
import java.nio.ByteBuffer;
import java.time.Duration;
import java.util.List;
import java.util.Set;
import java.util.concurrent.CancellationException;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CompletionException;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.Flow;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

/**
 * The SDK's HTTP: the three routes, one {@link HttpClient}, every request bounded by {@code
 * requestTimeout}. {@link HttpRequest#timeout} stops at the response headers, so the real bound is
 * {@code sendAsync(...).get(requestTimeout)} with a body handler that finishes when the body does:
 * one deadline covers connect, headers and body.
 *
 * <p>The ruleset and stream requests are tracked, so {@link #abortAll} ends them at {@code close};
 * the usage requests are not, so the final flush can still go out.
 *
 * <p>The only request headers set here are {@code Authorization}, {@code Accept}, {@code
 * If-None-Match} and {@code Content-Type}.
 */
final class Transport {

  /** How a request failed, when it did. */
  enum Fail {
    NONE,
    TIMEOUT,
    NETWORK,
    CANCELLED
  }

  /** A finished request. {@code body} is read for a 200 only. */
  record Reply(
      Fail fail, int status, String etag, String retryAfter, byte[] body, Throwable cause) {
    static Reply failed(Fail fail, Throwable cause) {
      return new Reply(fail, 0, null, null, null, cause);
    }
  }

  /** A stream whose headers arrived. The caller closes {@code body}. */
  record Opened(Fail fail, int status, String retryAfter, InputStream body, Throwable cause) {
    static Opened failed(Fail fail, Throwable cause) {
      return new Opened(fail, 0, null, null, cause);
    }
  }

  private static final byte[] NONE = new byte[0];

  private final HttpClient client;
  private final String base;
  private final String key;
  private final Duration timeout;
  private final Set<Runnable> aborters = ConcurrentHashMap.newKeySet();
  private volatile boolean aborted;

  Transport(ClientOptions options) {
    HttpClient.Builder builder =
        HttpClient.newBuilder()
            .version(HttpClient.Version.HTTP_1_1)
            .followRedirects(HttpClient.Redirect.NEVER)
            .connectTimeout(options.requestTimeout());
    if (options.sslContext() != null) {
      builder.sslContext(options.sslContext());
    }
    this.client = builder.build();
    this.base = options.url();
    this.key = options.sdkKey();
    this.timeout = options.requestTimeout();
  }

  /** Ends every tracked request, and refuses new ones. */
  void abortAll() {
    aborted = true;
    for (Runnable abort : aborters) {
      abort.run();
    }
  }

  /**
   * Tracks {@code abort} until {@link #untrack}. Returns false, and does not track, when {@link
   * #abortAll} has been called.
   */
  boolean track(Runnable abort) {
    aborters.add(abort);
    if (aborted) {
      aborters.remove(abort);
      return false;
    }
    return true;
  }

  void untrack(Runnable abort) {
    aborters.remove(abort);
  }

  /** {@code GET /sdk/v1/ruleset}, tracked. */
  Reply getRuleset(String ifNoneMatch) {
    try {
      HttpRequest.Builder request = request("/sdk/v1/ruleset");
      if (ifNoneMatch != null) {
        request.header("If-None-Match", ifNoneMatch);
      }
      return send(request.GET().build(), Transport::bodyOn200, true);
    } catch (RuntimeException e) {
      return Reply.failed(Fail.NETWORK, e);
    }
  }

  /** {@code POST /sdk/v1/events}, not tracked. */
  Reply postEvents(byte[] json) {
    try {
      HttpRequest request =
          request("/sdk/v1/events")
              .header("Content-Type", "application/json")
              .POST(HttpRequest.BodyPublishers.ofByteArray(json))
              .build();
      return send(request, info -> ignoring(), false);
    } catch (RuntimeException e) {
      return Reply.failed(Fail.NETWORK, e);
    }
  }

  /** {@code GET /sdk/v1/stream}, tracked until its headers arrive. */
  Opened openStream() {
    HttpRequest request;
    try {
      request = request("/sdk/v1/stream").header("Accept", "text/event-stream").GET().build();
    } catch (RuntimeException e) {
      return Opened.failed(Fail.NETWORK, e);
    }
    if (aborted) {
      return Opened.failed(Fail.CANCELLED, null);
    }
    CompletableFuture<HttpResponse<InputStream>> future =
        client.sendAsync(request, BodyHandlers.ofInputStream());
    Runnable abort = () -> future.cancel(true);
    if (!track(abort)) {
      future.cancel(true);
      return Opened.failed(Fail.CANCELLED, null);
    }
    try {
      HttpResponse<InputStream> response = future.get(timeout.toNanos(), TimeUnit.NANOSECONDS);
      return new Opened(
          Fail.NONE,
          response.statusCode(),
          response.headers().firstValue("Retry-After").orElse(null),
          response.body(),
          null);
    } catch (RuntimeException | ExecutionException | TimeoutException | InterruptedException e) {
      future.cancel(true);
      return Opened.failed(classify(e), e);
    } finally {
      untrack(abort);
    }
  }

  private HttpRequest.Builder request(String path) {
    return HttpRequest.newBuilder(URI.create(base + path))
        .timeout(timeout)
        .header("Authorization", "Bearer " + key);
  }

  private Reply send(HttpRequest request, BodyHandler<byte[]> handler, boolean tracked) {
    if (tracked && aborted) {
      return Reply.failed(Fail.CANCELLED, null);
    }
    CompletableFuture<HttpResponse<byte[]>> future = client.sendAsync(request, handler);
    Runnable abort = () -> future.cancel(true);
    if (tracked && !track(abort)) {
      future.cancel(true);
      return Reply.failed(Fail.CANCELLED, null);
    }
    try {
      HttpResponse<byte[]> response = future.get(timeout.toNanos(), TimeUnit.NANOSECONDS);
      return new Reply(
          Fail.NONE,
          response.statusCode(),
          response.headers().firstValue("ETag").orElse(null),
          response.headers().firstValue("Retry-After").orElse(null),
          response.body(),
          null);
    } catch (RuntimeException | ExecutionException | TimeoutException | InterruptedException e) {
      future.cancel(true);
      return Reply.failed(classify(e), e);
    } finally {
      if (tracked) {
        untrack(abort);
      }
    }
  }

  private static Fail classify(Throwable e) {
    if (e instanceof InterruptedException) {
      Thread.currentThread().interrupt();
      return Fail.CANCELLED;
    }
    if (e instanceof TimeoutException) {
      return Fail.TIMEOUT;
    }
    if (e instanceof CancellationException) {
      return Fail.CANCELLED;
    }
    Throwable cause = e;
    while ((cause instanceof ExecutionException || cause instanceof CompletionException)
        && cause.getCause() != null) {
      cause = cause.getCause();
    }
    if (cause instanceof CancellationException) {
      return Fail.CANCELLED;
    }
    if (cause instanceof HttpTimeoutException) {
      return Fail.TIMEOUT;
    }
    return Fail.NETWORK;
  }

  private static BodySubscriber<byte[]> bodyOn200(HttpResponse.ResponseInfo info) {
    return info.statusCode() == 200 ? HttpResponse.BodySubscribers.ofByteArray() : ignoring();
  }

  /** A body subscriber that finishes at once and reads nothing: the status alone decides. */
  private static BodySubscriber<byte[]> ignoring() {
    return new BodySubscriber<>() {
      private final CompletableFuture<byte[]> body = CompletableFuture.completedFuture(NONE);

      @Override
      public CompletableFuture<byte[]> getBody() {
        return body;
      }

      @Override
      public void onSubscribe(Flow.Subscription subscription) {
        subscription.cancel();
      }

      @Override
      public void onNext(List<ByteBuffer> item) {}

      @Override
      public void onError(Throwable throwable) {}

      @Override
      public void onComplete() {}
    };
  }

  /** Closes a stream body, ignoring the failure. */
  static void closeQuietly(InputStream in) {
    if (in != null) {
      try {
        in.close();
      } catch (IOException ignored) {
        // closing an aborted stream may fail; nothing to do
      }
    }
  }
}
