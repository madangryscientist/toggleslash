package com.github.madangryscientist.toggleslash.sdks.java;

import java.net.http.HttpResponse;
import java.net.http.HttpTimeoutException;
import java.nio.ByteBuffer;
import java.util.List;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.Flow;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;

/**
 * The body of the live update stream. It hands itself over the moment the headers arrive (a body
 * handler that answers only at the end of the body would never answer for a stream), queues each
 * piece of bytes, and asks for the next only after the reader took one, so memory stays bounded.
 * The 60 second watchdog is the timeout of the read.
 */
final class StreamBody implements HttpResponse.BodySubscriber<StreamBody> {
  private static final Object END = new Object();
  private static final Object ABORT = new Object();

  private final LinkedBlockingQueue<Object> queue = new LinkedBlockingQueue<>();

  /** What {@link Http} registers to stop this stream when the client closes. */
  final Runnable canceller = this::cancel;

  private volatile Flow.Subscription subscription;
  private volatile boolean cancelled;

  @Override
  public CompletableFuture<StreamBody> getBody() {
    return CompletableFuture.completedFuture(this);
  }

  @Override
  public void onSubscribe(Flow.Subscription subscription) {
    this.subscription = subscription;
    if (cancelled) {
      subscription.cancel();
      return;
    }
    subscription.request(1);
  }

  @Override
  public void onNext(List<ByteBuffer> buffers) {
    int total = 0;
    for (ByteBuffer buffer : buffers) {
      total += buffer.remaining();
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
  public void onError(Throwable throwable) {
    queue.add(throwable);
  }

  @Override
  public void onComplete() {
    queue.add(END);
  }

  /** Stops the stream and wakes a reader that is waiting. */
  void cancel() {
    cancelled = true;
    Flow.Subscription s = subscription;
    if (s != null) {
      s.cancel();
    }
    queue.add(ABORT);
  }

  /**
   * Reads the next piece of bytes.
   *
   * @param idleNanos how long to wait for a byte before the stream counts as dead
   * @return the bytes, or {@code null} when the server ended the stream
   * @throws Http.Failure if nothing came in time, the connection failed, or the stream was
   *     cancelled
   */
  byte[] read(long idleNanos) throws Http.Failure {
    Object item;
    try {
      item = queue.poll(idleNanos, TimeUnit.NANOSECONDS);
    } catch (InterruptedException interrupted) {
      throw new Http.Failure(Http.Kind.ABORTED);
    }
    if (item == null) {
      throw new Http.Failure(Http.Kind.TIMEOUT);
    }
    if (item == END) {
      return null;
    }
    if (item == ABORT) {
      throw new Http.Failure(Http.Kind.ABORTED);
    }
    if (item instanceof Throwable t) {
      if (cancelled) {
        throw new Http.Failure(Http.Kind.ABORTED);
      }
      throw new Http.Failure(
          t instanceof HttpTimeoutException ? Http.Kind.TIMEOUT : Http.Kind.NETWORK);
    }
    Flow.Subscription s = subscription;
    if (s != null) {
      s.request(1);
    }
    return (byte[]) item;
  }
}
