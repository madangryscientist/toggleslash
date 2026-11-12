package com.github.madangryscientist.toggleslash.sdks.java;

import java.net.http.HttpResponse;
import java.nio.ByteBuffer;
import java.util.List;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CompletionStage;
import java.util.concurrent.Flow;
import java.util.concurrent.LinkedBlockingQueue;

/**
 * The body of the stream. Its {@code getBody()} is complete at once, so the response future
 * completes when the headers arrive (a body that completes only at its end would never open).
 * Chunks go onto a queue, one request for the next at a time, so the queue never holds more than
 * one.
 */
final class StreamBody implements HttpResponse.BodySubscriber<Void> {
  /** The marker for a body that ended cleanly. */
  static final Object END = new Object();

  /** A body that failed. */
  record Failure(Throwable error) {}

  final LinkedBlockingQueue<Object> queue = new LinkedBlockingQueue<>();
  private volatile Flow.Subscription subscription;
  private volatile boolean cancelled;

  @Override
  public CompletionStage<Void> getBody() {
    return CompletableFuture.completedFuture(null);
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
    queue.add(new Failure(throwable));
  }

  @Override
  public void onComplete() {
    queue.add(END);
  }

  /** Asks for the next chunk; called once the last one has been taken. */
  void requestMore() {
    Flow.Subscription s = subscription;
    if (s != null && !cancelled) {
      s.request(1);
    }
  }

  /** Drops the connection. Safe to call twice, and before the subscription arrives. */
  void cancel() {
    cancelled = true;
    Flow.Subscription s = subscription;
    if (s != null) {
      s.cancel();
    }
  }
}
