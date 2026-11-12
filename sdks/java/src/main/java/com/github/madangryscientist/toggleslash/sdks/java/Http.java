package com.github.madangryscientist.toggleslash.sdks.java;

import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpHeaders;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.ByteBuffer;
import java.time.Duration;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.CancellationException;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.Flow;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

/**
 * The SDK's HTTP calls over {@link java.net.http.HttpClient}. Every call is bounded as a whole: the
 * platform's request timeout ends at the response headers, so the body is bounded here by waiting
 * on the future with a limit and cancelling it. Calls to the ruleset and stream routes are kept so
 * {@code close} can cancel them, since no client before Java 21 can be closed.
 */
final class Http {
  /** A finished request with its whole body. */
  record Answer(int status, HttpHeaders headers, byte[] body) {
    String header(String name) {
      return headers.firstValue(name).orElse(null);
    }
  }

  /** Thrown inside the SDK when {@code close} cancelled the call. It never leaves the package. */
  static final class Aborted extends RuntimeException {
    private static final long serialVersionUID = 1L;

    Aborted() {
      super(null, null, false, false);
    }
  }

  /** Something {@code close} can cancel. */
  private interface Cancellable {
    void cancel();
  }

  private final HttpClient client;
  private final Set<Cancellable> inflight = ConcurrentHashMap.newKeySet();
  private volatile boolean aborting;

  Http(HttpClient given, javax.net.ssl.SSLContext ssl) {
    if (given != null) {
      this.client = given;
    } else {
      HttpClient.Builder builder =
          HttpClient.newBuilder()
              .version(HttpClient.Version.HTTP_1_1)
              .followRedirects(HttpClient.Redirect.NEVER);
      if (ssl != null) {
        builder.sslContext(ssl);
      }
      this.client = builder.build();
    }
  }

  /** Cancels every ruleset and stream call in flight, and any made after this. */
  void abortAll() {
    aborting = true;
    for (Cancellable call : inflight) {
      call.cancel();
    }
  }

  private static HttpRequest build(
      String method, String url, Map<String, String> headers, byte[] body) {
    HttpRequest.Builder builder = HttpRequest.newBuilder(URI.create(url));
    for (Map.Entry<String, String> header : headers.entrySet()) {
      builder.header(header.getKey(), header.getValue());
    }
    if (body == null) {
      return builder.GET().build();
    }
    return builder.method(method, HttpRequest.BodyPublishers.ofByteArray(body)).build();
  }

  private static ToggleSlashException network(Throwable cause) {
    return new ToggleSlashException(
        "network", "request failed: " + cause.getClass().getSimpleName(), cause);
  }

  private static ToggleSlashException timeout(String what) {
    return new ToggleSlashException(Status.TIMEOUT, what + " timed out", null);
  }

  /**
   * One request, body included, within {@code timeoutMs}.
   *
   * @param abortable whether {@code close} cancels it
   * @throws ToggleSlashException with code {@code timeout} or {@code network}
   * @throws Aborted when {@code close} cancelled the call
   */
  Answer request(
      String method,
      String url,
      Map<String, String> headers,
      byte[] body,
      long timeoutMs,
      boolean abortable) {
    if (abortable && aborting) {
      throw new Aborted();
    }
    CompletableFuture<HttpResponse<byte[]>> future;
    try {
      future =
          client.sendAsync(
              build(method, url, headers, body), HttpResponse.BodyHandlers.ofByteArray());
    } catch (RuntimeException e) {
      throw network(e);
    }
    Cancellable handle = () -> future.cancel(true);
    if (abortable) {
      inflight.add(handle);
      if (aborting) {
        future.cancel(true);
      }
    }
    try {
      HttpResponse<byte[]> response = future.get(timeoutMs, TimeUnit.MILLISECONDS);
      return new Answer(response.statusCode(), response.headers(), response.body());
    } catch (TimeoutException e) {
      future.cancel(true);
      throw timeout(method + " " + route(url));
    } catch (CancellationException e) {
      throw new Aborted();
    } catch (InterruptedException e) {
      future.cancel(true);
      Thread.currentThread().interrupt();
      throw new Aborted();
    } catch (ExecutionException e) {
      Throwable cause = e.getCause() == null ? e : e.getCause();
      if (cause instanceof java.net.http.HttpTimeoutException) {
        throw timeout(method + " " + route(url));
      }
      if (cause instanceof CancellationException) {
        throw new Aborted();
      }
      throw network(cause);
    } finally {
      inflight.remove(handle);
    }
  }

  private static String route(String url) {
    int at = url.indexOf("/sdk/v1/");
    return at < 0 ? "request" : url.substring(at);
  }

  /** A stream whose headers have arrived. */
  static final class Opened implements Cancellable {
    final int status;
    final HttpHeaders headers;
    final StreamBody body;

    Opened(int status, HttpHeaders headers, StreamBody body) {
      this.status = status;
      this.headers = headers;
      this.body = body;
    }

    String header(String name) {
      return headers.firstValue(name).orElse(null);
    }

    @Override
    public void cancel() {
      body.cancel();
    }
  }

  /**
   * Opens a stream: returns once the response headers have arrived, bounded by {@code openMs}. The
   * body is read through {@link StreamBody#next}.
   */
  Opened open(String url, Map<String, String> headers, long openMs) {
    if (aborting) {
      throw new Aborted();
    }
    CompletableFuture<HttpResponse<StreamBody>> future;
    try {
      future = client.sendAsync(build("GET", url, headers, null), info -> new StreamBody());
    } catch (RuntimeException e) {
      throw network(e);
    }
    Cancellable pending = () -> future.cancel(true);
    inflight.add(pending);
    if (aborting) {
      future.cancel(true);
    }
    try {
      HttpResponse<StreamBody> response = future.get(openMs, TimeUnit.MILLISECONDS);
      Opened opened = new Opened(response.statusCode(), response.headers(), response.body());
      inflight.add(opened);
      if (aborting) {
        opened.cancel();
      }
      return opened;
    } catch (TimeoutException e) {
      future.cancel(true);
      throw timeout("GET /sdk/v1/stream");
    } catch (CancellationException e) {
      throw new Aborted();
    } catch (InterruptedException e) {
      future.cancel(true);
      Thread.currentThread().interrupt();
      throw new Aborted();
    } catch (ExecutionException e) {
      Throwable cause = e.getCause() == null ? e : e.getCause();
      if (cause instanceof java.net.http.HttpTimeoutException) {
        throw timeout("GET /sdk/v1/stream");
      }
      if (cause instanceof CancellationException) {
        throw new Aborted();
      }
      throw network(cause);
    } finally {
      inflight.remove(pending);
    }
  }

  /** Forgets a stream that has ended. */
  void release(Opened opened) {
    inflight.remove(opened);
    opened.cancel();
  }

  /**
   * A response body handed over at once: {@code getBody} is complete before any byte arrives, so
   * the request's future completes at the headers and the reader pulls the chunks from a queue. One
   * chunk is requested at a time, so a slow reader slows the connection instead of filling memory.
   */
  static final class StreamBody implements HttpResponse.BodySubscriber<StreamBody> {
    private static final Object END = new Object();
    private static final Object ABORT = new Object();

    private final LinkedBlockingQueue<Object> queue = new LinkedBlockingQueue<>();
    private volatile Flow.Subscription subscription;
    private volatile boolean cancelled;

    @Override
    public CompletableFuture<StreamBody> getBody() {
      return CompletableFuture.completedFuture(this);
    }

    @Override
    public void onSubscribe(Flow.Subscription value) {
      this.subscription = value;
      if (cancelled) {
        value.cancel();
        return;
      }
      value.request(1);
    }

    @Override
    public void onNext(List<ByteBuffer> items) {
      int total = 0;
      for (ByteBuffer item : items) {
        total += item.remaining();
      }
      byte[] chunk = new byte[total];
      int at = 0;
      for (ByteBuffer item : items) {
        int n = item.remaining();
        item.get(chunk, at, n);
        at += n;
      }
      queue.add(chunk);
    }

    @Override
    public void onError(Throwable failure) {
      queue.add(failure);
    }

    @Override
    public void onComplete() {
      queue.add(END);
    }

    /**
     * Stops the body: the connection is dropped and a waiting reader wakes with {@link Aborted}.
     */
    void cancel() {
      cancelled = true;
      Flow.Subscription current = subscription;
      if (current != null) {
        current.cancel();
      }
      queue.add(ABORT);
    }

    /**
     * The next chunk, or null at a clean end.
     *
     * @throws ToggleSlashException with code {@code timeout} after {@code idleMs} without a byte,
     *     or {@code network} when the connection fails
     * @throws Aborted when {@link #cancel} ran
     */
    byte[] next(long idleMs) {
      Object item;
      try {
        item = queue.poll(idleMs, TimeUnit.MILLISECONDS);
      } catch (InterruptedException e) {
        Thread.currentThread().interrupt();
        throw new Aborted();
      }
      if (item == null) {
        throw timeout("the stream");
      }
      if (item == END) {
        queue.add(END);
        return null;
      }
      if (item == ABORT) {
        queue.add(ABORT);
        throw new Aborted();
      }
      if (item instanceof Throwable) {
        Throwable failure = (Throwable) item;
        if (failure instanceof CancellationException) {
          throw new Aborted();
        }
        queue.add(failure);
        throw network(failure);
      }
      Flow.Subscription current = subscription;
      if (current != null && !cancelled) {
        current.request(1);
      }
      return (byte[]) item;
    }
  }

  /** Whole milliseconds of a duration, saturating. */
  static long millis(Duration value) {
    try {
      return Math.max(0, value.toMillis());
    } catch (ArithmeticException e) {
      return Long.MAX_VALUE;
    }
  }
}
