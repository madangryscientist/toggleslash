package com.github.madangryscientist.toggleslash.sdks.java;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.net.http.HttpTimeoutException;
import java.nio.ByteBuffer;
import java.util.List;
import java.util.concurrent.CancellationException;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CompletionStage;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.Flow;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

/**
 * One HTTP exchange. The response is handed over as soon as its headers arrive, and the body is
 * then read a chunk at a time with a deadline, so a request can be bounded as a whole (headers and
 * body) and a stream can be read for as long as it lives. {@link #abort()} ends the exchange from
 * any thread and wakes a reader.
 */
final class Exchange {
  private static final Object END = new Object();
  private static final Object ABORT = new Object();

  private final Sink sink = new Sink();
  private volatile CompletableFuture<HttpResponse<Sink>> future;
  private volatile boolean aborted;
  private int status;
  private java.net.http.HttpHeaders headers;

  /** The body subscriber: chunks go on a queue, one at a time. */
  private static final class Sink implements HttpResponse.BodySubscriber<Sink> {
    private final LinkedBlockingQueue<Object> queue = new LinkedBlockingQueue<>();
    private volatile Flow.Subscription subscription;
    private volatile boolean cancelled;

    @Override
    public void onSubscribe(Flow.Subscription next) {
      subscription = next;
      if (cancelled) {
        next.cancel();
      } else {
        next.request(1);
      }
    }

    @Override
    public void onNext(List<ByteBuffer> buffers) {
      int total = 0;
      for (ByteBuffer buffer : buffers) {
        total += buffer.remaining();
      }
      if (total == 0) {
        request();
        return;
      }
      byte[] bytes = new byte[total];
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
      queue.add(new Failure(error));
    }

    @Override
    public void onComplete() {
      queue.add(END);
    }

    @Override
    public CompletionStage<Sink> getBody() {
      return CompletableFuture.completedFuture(this);
    }

    void request() {
      Flow.Subscription s = subscription;
      if (s != null && !cancelled) {
        s.request(1);
      }
    }

    void cancel() {
      cancelled = true;
      Flow.Subscription s = subscription;
      if (s != null) {
        s.cancel();
      }
      queue.add(ABORT);
    }
  }

  private record Failure(Throwable cause) {}

  /**
   * Sends the request and waits for the response headers.
   *
   * @param client the client
   * @param request the request
   * @param deadlineNanos the {@code System.nanoTime} by which the headers must be here
   * @throws IOException when the exchange failed, was aborted, or the headers were not valid
   * @throws TimeoutException when the headers did not arrive in time
   * @throws InterruptedException when the thread is interrupted
   */
  void start(HttpClient client, HttpRequest request, long deadlineNanos)
      throws IOException, TimeoutException, InterruptedException {
    if (aborted) {
      throw new IOException("aborted");
    }
    CompletableFuture<HttpResponse<Sink>> sent = client.sendAsync(request, info -> sink);
    future = sent;
    if (aborted) {
      sent.cancel(true);
    }
    long left = deadlineNanos - System.nanoTime();
    if (left <= 0) {
      abort();
      throw new TimeoutException();
    }
    try {
      HttpResponse<Sink> response = sent.get(left, TimeUnit.NANOSECONDS);
      status = response.statusCode();
      headers = response.headers();
    } catch (TimeoutException e) {
      abort();
      throw e;
    } catch (CancellationException e) {
      throw new IOException("aborted", e);
    } catch (ExecutionException e) {
      Throwable cause = e.getCause();
      abort();
      if (cause instanceof HttpTimeoutException timeout) {
        throw timeout;
      }
      if (cause instanceof IOException io) {
        throw io;
      }
      throw new IOException(cause);
    }
    if (aborted) {
      throw new IOException("aborted");
    }
  }

  int status() {
    return status;
  }

  /**
   * The first value of a response header.
   *
   * @param name the header name
   * @return the value, or null
   */
  String header(String name) {
    return headers == null ? null : headers.firstValue(name).orElse(null);
  }

  /**
   * The next chunk of the body.
   *
   * @param timeoutNanos how long to wait for it
   * @return the bytes, or null at the end of the body
   * @throws IOException when the body failed or the exchange was aborted
   * @throws TimeoutException when no chunk came in time
   * @throws InterruptedException when the thread is interrupted
   */
  byte[] next(long timeoutNanos) throws IOException, TimeoutException, InterruptedException {
    if (aborted) {
      throw new IOException("aborted");
    }
    Object item = sink.queue.poll(Math.max(timeoutNanos, 0), TimeUnit.NANOSECONDS);
    if (item == null) {
      throw new TimeoutException();
    }
    if (item == END) {
      sink.queue.add(END);
      return null;
    }
    if (item == ABORT) {
      throw new IOException("aborted");
    }
    if (item instanceof Failure failure) {
      if (failure.cause() instanceof IOException io) {
        throw io;
      }
      throw new IOException(failure.cause());
    }
    sink.request();
    return (byte[]) item;
  }

  /**
   * Reads the whole body.
   *
   * @param deadlineNanos the {@code System.nanoTime} by which it must be read
   * @return the bytes
   * @throws IOException when the body failed or the exchange was aborted
   * @throws TimeoutException when the body did not end in time
   * @throws InterruptedException when the thread is interrupted
   */
  byte[] readAll(long deadlineNanos) throws IOException, TimeoutException, InterruptedException {
    ByteArrayOutputStream out = new ByteArrayOutputStream();
    while (true) {
      byte[] chunk = next(deadlineNanos - System.nanoTime());
      if (chunk == null) {
        return out.toByteArray();
      }
      out.write(chunk, 0, chunk.length);
    }
  }

  /** Ends the exchange: cancels the request and the body, and wakes a reader. */
  void abort() {
    aborted = true;
    CompletableFuture<HttpResponse<Sink>> sent = future;
    if (sent != null) {
      sent.cancel(true);
    }
    sink.cancel();
  }
}
