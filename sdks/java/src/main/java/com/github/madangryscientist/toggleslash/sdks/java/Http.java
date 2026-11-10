package com.github.madangryscientist.toggleslash.sdks.java;

import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.net.http.HttpResponse.BodyHandler;
import java.net.http.HttpResponse.BodyHandlers;
import java.nio.ByteBuffer;
import java.time.Duration;
import java.util.List;
import java.util.Map;
import java.util.Optional;
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
 * The SDK's HTTP: HTTP/1.1, no redirects, every request bounded as a whole, and every request in
 * flight can be abandoned with {@link #abort()}. Only the headers a caller passes are set here; the
 * JDK adds {@code Host}, {@code User-Agent} and {@code Content-Length} on its own.
 */
final class Http {
  /** Why a request did not give an answer. */
  enum Kind {
    TIMEOUT,
    NETWORK,
    ABORTED
  }

  /** A request that gave no answer. The message is fixed text: no URL, key or context. */
  static final class Failure extends Exception {
    private static final long serialVersionUID = 1L;

    private final transient Kind kind;

    Failure(Kind kind) {
      super(kind.name().toLowerCase(java.util.Locale.ROOT), null, false, false);
      this.kind = kind;
    }

    Kind kind() {
      return kind;
    }
  }

  /** An answer: the status, the body (read only for a 200), and the headers the SDK uses. */
  record Reply(int status, byte[] body, Optional<String> etag, Optional<String> retryAfter) {}

  private static final byte[] NO_BODY = new byte[0];

  private final HttpClient client;
  private final Set<CompletableFuture<?>> inflight = ConcurrentHashMap.newKeySet();
  private final Set<Stream> streams = ConcurrentHashMap.newKeySet();
  private volatile boolean aborted;

  Http(HttpClient client) {
    this.client = client;
  }

  /** A client that speaks HTTP/1.1 and never follows a redirect. */
  static HttpClient newClient(Duration connectTimeout) {
    return HttpClient.newBuilder()
        .version(HttpClient.Version.HTTP_1_1)
        .followRedirects(HttpClient.Redirect.NEVER)
        .connectTimeout(connectTimeout)
        .build();
  }

  /** Abandons every request in flight and refuses new ones. */
  void abort() {
    aborted = true;
    for (CompletableFuture<?> future : inflight) {
      future.cancel(true);
    }
    for (Stream stream : streams) {
      stream.abort();
    }
  }

  private HttpRequest request(
      String url, String method, String body, Map<String, String> headers, Duration timeout)
      throws Failure {
    try {
      HttpRequest.Builder builder = HttpRequest.newBuilder(URI.create(url)).timeout(timeout);
      for (Map.Entry<String, String> header : headers.entrySet()) {
        builder.header(header.getKey(), header.getValue());
      }
      if (body == null) {
        return builder.GET().build();
      }
      return builder.method(method, HttpRequest.BodyPublishers.ofString(body)).build();
    } catch (RuntimeException e) {
      throw new Failure(Kind.NETWORK);
    }
  }

  private static BodyHandler<byte[]> wholeBody() {
    return info ->
        info.statusCode() == 200
            ? BodyHandlers.ofByteArray().apply(info)
            : BodyHandlers.replacing(NO_BODY).apply(info);
  }

  private static Reply reply(HttpResponse<byte[]> response) {
    return new Reply(
        response.statusCode(),
        response.body(),
        response.headers().firstValue("ETag"),
        response.headers().firstValue("Retry-After"));
  }

  /** A GET whose whole answer, body included, arrives within {@code timeout}. */
  Reply get(String url, Map<String, String> headers, Duration timeout) throws Failure {
    return send(request(url, "GET", null, headers, timeout), timeout);
  }

  /** A POST of {@code body} that is answered within {@code timeout}. */
  Reply post(String url, String body, Map<String, String> headers, Duration timeout)
      throws Failure {
    return send(request(url, "POST", body, headers, timeout), timeout);
  }

  private Reply send(HttpRequest request, Duration timeout) throws Failure {
    if (aborted) {
      throw new Failure(Kind.ABORTED);
    }
    CompletableFuture<HttpResponse<byte[]>> future;
    try {
      future = client.sendAsync(request, wholeBody());
    } catch (RuntimeException e) {
      throw new Failure(Kind.NETWORK);
    }
    inflight.add(future);
    try {
      if (aborted) {
        future.cancel(true);
      }
      return reply(future.get(timeout.toNanos(), TimeUnit.NANOSECONDS));
    } catch (TimeoutException e) {
      future.cancel(true);
      throw new Failure(Kind.TIMEOUT);
    } catch (CancellationException e) {
      throw new Failure(aborted ? Kind.ABORTED : Kind.NETWORK);
    } catch (InterruptedException e) {
      future.cancel(true);
      Thread.currentThread().interrupt();
      throw new Failure(Kind.ABORTED);
    } catch (ExecutionException e) {
      throw new Failure(
          e.getCause() instanceof java.net.http.HttpTimeoutException ? Kind.TIMEOUT : Kind.NETWORK);
    } finally {
      inflight.remove(future);
    }
  }

  /** What opening the stream gave: a body to read on a 200, otherwise just the answer's headers. */
  record Opened(int status, Optional<String> retryAfter, Stream stream) {}

  /** Opens a stream; the open (not the stream) is bounded by {@code timeout}. */
  Opened open(String url, Map<String, String> headers, Duration timeout) throws Failure {
    HttpRequest request = request(url, "GET", null, headers, timeout);
    if (aborted) {
      throw new Failure(Kind.ABORTED);
    }
    Stream stream = new Stream();
    // The answer completes with the headers, because the stream is its own body subscriber and
    // hands
    // itself over at once; BodyHandlers.fromSubscriber would hold the answer until the stream
    // ended.
    BodyHandler<Stream> handler =
        info ->
            info.statusCode() == 200
                ? stream
                : HttpResponse.BodySubscribers.replacing((Stream) null);
    CompletableFuture<HttpResponse<Stream>> future;
    try {
      future = client.sendAsync(request, handler);
    } catch (RuntimeException e) {
      throw new Failure(Kind.NETWORK);
    }
    inflight.add(future);
    streams.add(stream);
    try {
      if (aborted) {
        future.cancel(true);
      }
      HttpResponse<Stream> response = future.get(timeout.toNanos(), TimeUnit.NANOSECONDS);
      Optional<String> retryAfter = response.headers().firstValue("Retry-After");
      if (response.statusCode() != 200) {
        streams.remove(stream);
        return new Opened(response.statusCode(), retryAfter, null);
      }
      if (aborted) {
        stream.abort();
      }
      return new Opened(200, retryAfter, stream);
    } catch (TimeoutException e) {
      future.cancel(true);
      stream.abort();
      streams.remove(stream);
      throw new Failure(Kind.TIMEOUT);
    } catch (CancellationException e) {
      streams.remove(stream);
      throw new Failure(aborted ? Kind.ABORTED : Kind.NETWORK);
    } catch (InterruptedException e) {
      future.cancel(true);
      stream.abort();
      streams.remove(stream);
      Thread.currentThread().interrupt();
      throw new Failure(Kind.ABORTED);
    } catch (ExecutionException e) {
      streams.remove(stream);
      throw new Failure(
          e.getCause() instanceof java.net.http.HttpTimeoutException ? Kind.TIMEOUT : Kind.NETWORK);
    } finally {
      inflight.remove(future);
    }
  }

  /** Forgets a stream that has been read to its end or dropped. */
  void release(Stream stream) {
    stream.abort();
    streams.remove(stream);
  }

  /**
   * The body of an open stream, handed over a chunk at a time with back-pressure: one chunk is
   * requested, and the next only once the reader has taken it. Nothing here blocks the JDK's
   * threads.
   */
  static final class Stream implements HttpResponse.BodySubscriber<Stream> {
    /** The stream ended cleanly. */
    static final Object END = new Object();

    /** The stream was abandoned. */
    static final Object ABORTED = new Object();

    private final LinkedBlockingQueue<Object> queue = new LinkedBlockingQueue<>();
    private Flow.Subscription subscription;
    private boolean cancelled;

    @Override
    public CompletableFuture<Stream> getBody() {
      return CompletableFuture.completedFuture(this);
    }

    @Override
    public synchronized void onSubscribe(Flow.Subscription s) {
      if (cancelled) {
        s.cancel();
        return;
      }
      subscription = s;
      s.request(1);
    }

    @Override
    public void onNext(List<ByteBuffer> buffers) {
      int size = 0;
      for (ByteBuffer buffer : buffers) {
        size += buffer.remaining();
      }
      byte[] bytes = new byte[size];
      int at = 0;
      for (ByteBuffer buffer : buffers) {
        int n = buffer.remaining();
        buffer.get(bytes, at, n);
        at += n;
      }
      queue.add(bytes);
    }

    @Override
    public void onError(Throwable error) {
      queue.add(error);
    }

    @Override
    public void onComplete() {
      queue.add(END);
    }

    /**
     * The next chunk ({@code byte[]}), {@link #END}, {@link #ABORTED}, a {@code Throwable} that
     * ended the stream, or null when {@code timeout} passed with nothing.
     */
    Object next(Duration timeout) throws InterruptedException {
      Object item = queue.poll(timeout.toNanos(), TimeUnit.NANOSECONDS);
      if (item instanceof byte[]) {
        synchronized (this) {
          if (subscription != null && !cancelled) {
            subscription.request(1);
          }
        }
      }
      return item;
    }

    /** Stops the stream and wakes a reader that is waiting. */
    synchronized void abort() {
      if (cancelled) {
        return;
      }
      cancelled = true;
      if (subscription != null) {
        subscription.cancel();
      }
      queue.add(ABORTED);
    }
  }
}
