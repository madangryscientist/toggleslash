package com.github.madangryscientist.toggleslash.sdks.java;

import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.net.http.HttpTimeoutException;
import java.nio.ByteBuffer;
import java.time.Duration;
import java.util.List;
import java.util.Set;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.CancellationException;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.Flow;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

/**
 * Requests over the JDK's {@link HttpClient}. Each request is bounded as a whole, and each one in
 * flight can be abandoned with {@link #abortAll}.
 */
final class Http {
  /** A failed request: its {@code lastError} code. */
  static final class Failure extends Exception {
    private static final long serialVersionUID = 1L;
    final String code;

    Failure(String code) {
      super(code, null, false, false);
      this.code = code;
    }
  }

  /** A whole answer. */
  static final class Response {
    final int status;
    final String etag;
    final String retryAfter;
    final byte[] body;

    Response(int status, String etag, String retryAfter, byte[] body) {
      this.status = status;
      this.etag = etag;
      this.retryAfter = retryAfter;
      this.body = body;
    }
  }

  /** The marker for the end of a stream body. */
  static final Object END = new Object();

  /** The marker for a stream abandoned on purpose. */
  static final Object ABORTED = new Object();

  /** An open stream answer: status, then chunks on a queue. */
  static final class StreamResponse {
    final int status;
    final String retryAfter;
    final BlockingQueue<Object> queue;
    private final Runnable cancel;
    private volatile Flow.Subscription subscription;

    StreamResponse(int status, String retryAfter, BlockingQueue<Object> queue, Runnable cancel) {
      this.status = status;
      this.retryAfter = retryAfter;
      this.queue = queue;
      this.cancel = cancel;
    }

    /** Asks for the next chunk. */
    void more() {
      Flow.Subscription current = subscription;
      if (current != null) {
        current.request(1);
      }
    }

    void cancel() {
      cancel.run();
    }
  }

  private final HttpClient client;
  private final Set<Runnable> aborts = ConcurrentHashMap.newKeySet();

  Http(HttpClient client) {
    this.client = client;
  }

  static HttpClient newClient(Duration connectTimeout) {
    return HttpClient.newBuilder()
        .version(HttpClient.Version.HTTP_1_1)
        .connectTimeout(connectTimeout)
        .build();
  }

  /** Cancels every request in flight. */
  void abortAll() {
    for (Runnable abort : aborts) {
      try {
        abort.run();
      } catch (RuntimeException ignored) {
        // Nothing else to do for a request that cannot be cancelled.
      }
    }
  }

  private static Failure failureOf(Throwable error) {
    Throwable cause = error;
    while (cause != null) {
      if (cause instanceof HttpTimeoutException || cause instanceof TimeoutException) {
        return new Failure(Status.TIMEOUT);
      }
      cause = cause.getCause();
    }
    return new Failure(Status.NETWORK);
  }

  private static String first(HttpResponse<?> response, String name) {
    return response.headers().firstValue(name).orElse(null);
  }

  /** One request, answered whole, bounded by the timeout from the send to the last byte. */
  Response send(HttpRequest.Builder builder, Duration timeout) throws Failure {
    CompletableFuture<HttpResponse<byte[]>> future;
    Runnable abort;
    try {
      HttpRequest request = builder.timeout(timeout).build();
      future = client.sendAsync(request, HttpResponse.BodyHandlers.ofByteArray());
      CompletableFuture<HttpResponse<byte[]>> started = future;
      abort = () -> started.cancel(true);
    } catch (RuntimeException e) {
      throw new Failure(Status.NETWORK);
    }
    aborts.add(abort);
    try {
      HttpResponse<byte[]> response =
          future.get(Math.max(1, timeout.toMillis()), TimeUnit.MILLISECONDS);
      return new Response(
          response.statusCode(),
          first(response, "ETag"),
          first(response, "Retry-After"),
          response.body());
    } catch (TimeoutException e) {
      future.cancel(true);
      throw new Failure(Status.TIMEOUT);
    } catch (ExecutionException e) {
      throw failureOf(e);
    } catch (CancellationException e) {
      throw new Failure(Status.NETWORK);
    } catch (InterruptedException e) {
      future.cancel(true);
      Thread.currentThread().interrupt();
      throw new Failure(Status.NETWORK);
    } finally {
      aborts.remove(abort);
    }
  }

  /**
   * Opens a stream: the answer is bounded by the timeout up to the headers, and the body then
   * arrives on the queue as byte arrays, then {@link #END} or a Throwable or {@link #ABORTED}.
   */
  StreamResponse open(HttpRequest.Builder builder, Duration timeout) throws Failure {
    CompletableFuture<HttpResponse<Flow.Publisher<List<ByteBuffer>>>> future;
    try {
      HttpRequest request = builder.build();
      future = client.sendAsync(request, HttpResponse.BodyHandlers.ofPublisher());
    } catch (RuntimeException e) {
      throw new Failure(Status.NETWORK);
    }
    BlockingQueue<Object> queue = new LinkedBlockingQueue<>();
    StreamResponse[] holder = new StreamResponse[1];
    Runnable abort =
        () -> {
          future.cancel(true);
          StreamResponse current = holder[0];
          if (current != null) {
            Flow.Subscription sub = current.subscription;
            if (sub != null) {
              sub.cancel();
            }
          }
          queue.offer(ABORTED);
        };
    aborts.add(abort);
    HttpResponse<Flow.Publisher<List<ByteBuffer>>> response;
    try {
      response = future.get(Math.max(1, timeout.toMillis()), TimeUnit.MILLISECONDS);
    } catch (TimeoutException e) {
      future.cancel(true);
      aborts.remove(abort);
      throw new Failure(Status.TIMEOUT);
    } catch (ExecutionException e) {
      aborts.remove(abort);
      throw failureOf(e);
    } catch (CancellationException e) {
      aborts.remove(abort);
      throw new Failure(Status.NETWORK);
    } catch (InterruptedException e) {
      future.cancel(true);
      aborts.remove(abort);
      Thread.currentThread().interrupt();
      throw new Failure(Status.NETWORK);
    }
    Runnable cancel =
        () -> {
          abort.run();
          aborts.remove(abort);
        };
    StreamResponse stream =
        new StreamResponse(response.statusCode(), first(response, "Retry-After"), queue, cancel);
    holder[0] = stream;
    boolean keep = response.statusCode() == 200;
    response
        .body()
        .subscribe(
            new Flow.Subscriber<List<ByteBuffer>>() {
              @Override
              public void onSubscribe(Flow.Subscription subscription) {
                stream.subscription = subscription;
                if (keep) {
                  subscription.request(1);
                } else {
                  subscription.cancel();
                }
              }

              @Override
              public void onNext(List<ByteBuffer> item) {
                int size = 0;
                for (ByteBuffer buffer : item) {
                  size += buffer.remaining();
                }
                if (size == 0) {
                  stream.more();
                  return;
                }
                byte[] chunk = new byte[size];
                int at = 0;
                for (ByteBuffer buffer : item) {
                  int n = buffer.remaining();
                  buffer.get(chunk, at, n);
                  at += n;
                }
                queue.offer(chunk);
              }

              @Override
              public void onError(Throwable error) {
                queue.offer(error);
              }

              @Override
              public void onComplete() {
                queue.offer(END);
              }
            });
    return stream;
  }

  /** Releases the in-flight record of a stream that has ended. */
  void finish(StreamResponse stream) {
    stream.cancel();
  }
}
